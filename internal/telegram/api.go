package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
)

type APIError struct {
	Code        int
	Description string
	RetryAfter  int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Telegram API %d: %s", e.Code, e.Description)
}

func (e *APIError) Forbidden() bool {
	return e.Code == 403
}

func (e *APIError) BadRequest() bool {
	return e.Code == 400
}

type Client struct {
	token string
	http  *http.Client
}

func NewClient(token string, client *http.Client) *Client {
	return &Client{token: token, http: client}
}

func (c *Client) call(ctx context.Context, method string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+c.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return err
	}

	defer response.Body.Close()
	return decodeResponse(response.Body, target)
}

func decodeResponse(reader io.Reader, target any) error {
	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.NewDecoder(reader).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return &APIError{Code: envelope.ErrorCode, Description: envelope.Description, RetryAfter: envelope.Parameters.RetryAfter}
	}
	if target != nil && len(envelope.Result) > 0 {
		return json.Unmarshal(envelope.Result, target)
	}
	return nil
}

type inlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}
type keyboard struct {
	InlineKeyboard [][]inlineButton `json:"inline_keyboard"`
}

func button(text, data string) inlineButton {
	return inlineButton{Text: text, CallbackData: data}
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, markup *keyboard) (int, error) {
	payload := map[string]any{"chat_id": chatID, "text": text, "parse_mode": "HTML", "link_preview_options": map[string]bool{"is_disabled": true}}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	var message struct {
		MessageID int `json:"message_id"`
	}
	err := c.call(ctx, "sendMessage", payload, &message)
	return message.MessageID, err
}

func (c *Client) EditMessage(ctx context.Context, chatID int64, messageID int, text string, markup *keyboard) error {
	payload := map[string]any{"chat_id": chatID, "message_id": messageID, "text": text, "parse_mode": "HTML"}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	return c.call(ctx, "editMessageText", payload, nil)
}

func (c *Client) AnswerCallback(ctx context.Context, id, text string, alert bool) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text, "show_alert": alert}, nil)
}

type Photo struct {
	Content  []byte
	Filename string
}
type tgMessage struct {
	Photo []struct {
		FileID string `json:"file_id"`
	} `json:"photo"`
}

func (c *Client) SendPhotos(ctx context.Context, chatID int64, caption string, photos []Photo, fileIDs []string) ([]string, error) {
	if len(fileIDs) > 0 {
		return fileIDs, c.sendCachedPhotos(ctx, chatID, caption, fileIDs)
	}
	if len(photos) == 0 {
		_, err := c.SendMessage(ctx, chatID, caption, nil)
		return nil, err
	}
	if len(photos) == 1 {
		return c.sendOnePhoto(ctx, chatID, caption, photos[0])
	}
	return c.sendUploadedAlbum(ctx, chatID, caption, photos)
}

func (c *Client) sendCachedPhotos(ctx context.Context, chatID int64, caption string, ids []string) error {
	if len(ids) == 1 {
		return c.call(ctx, "sendPhoto", map[string]any{"chat_id": chatID, "photo": ids[0], "caption": caption, "parse_mode": "HTML"}, nil)
	}
	media := make([]map[string]any, len(ids))
	for i, id := range ids {
		media[i] = map[string]any{"type": "photo", "media": id}
		if i == 0 {
			media[i]["caption"] = caption
			media[i]["parse_mode"] = "HTML"
		}
	}
	return c.call(ctx, "sendMediaGroup", map[string]any{"chat_id": chatID, "media": media}, nil)
}

func (c *Client) sendOnePhoto(ctx context.Context, chatID int64, caption string, photo Photo) ([]string, error) {
	var message tgMessage
	err := c.multipart(ctx, "sendPhoto", map[string]string{"chat_id": strconv.FormatInt(chatID, 10), "caption": caption, "parse_mode": "HTML"}, map[string]Photo{"photo": photo}, &message)
	if err != nil {
		return nil, err
	}
	if len(message.Photo) == 0 {
		return nil, nil
	}
	return []string{message.Photo[len(message.Photo)-1].FileID}, nil
}

func (c *Client) sendUploadedAlbum(ctx context.Context, chatID int64, caption string, photos []Photo) ([]string, error) {
	media := make([]map[string]any, len(photos))
	files := map[string]Photo{}
	for i, photo := range photos {
		name := "photo" + strconv.Itoa(i)
		media[i] = map[string]any{"type": "photo", "media": "attach://" + name}
		if i == 0 {
			media[i]["caption"] = caption
			media[i]["parse_mode"] = "HTML"
		}
		files[name] = photo
	}
	encoded, _ := json.Marshal(media)
	var messages []tgMessage
	err := c.multipart(ctx, "sendMediaGroup", map[string]string{"chat_id": strconv.FormatInt(chatID, 10), "media": string(encoded)}, files, &messages)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		if len(message.Photo) > 0 {
			ids = append(ids, message.Photo[len(message.Photo)-1].FileID)
		}
	}
	if len(ids) != len(photos) {
		return nil, nil
	}
	return ids, nil
}

func (c *Client) multipart(ctx context.Context, method string, fields map[string]string, files map[string]Photo, target any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return err
		}
	}
	for field, photo := range files {
		part, err := writer.CreateFormFile(field, photo.Filename)
		if err != nil {
			return err
		}
		if _, err := part.Write(photo.Content); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+c.token+"/"+method, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return decodeResponse(response.Body, target)
}
