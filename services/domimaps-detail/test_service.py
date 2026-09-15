import unittest

import service


class FakeLocator:
    def __init__(self, value=" Apraksts ar garumzīmēm. \n"):
        self.value = value
        self.wait = None
        self.inner_timeout = None

    def wait_for(self, **kwargs):
        self.wait = kwargs

    def inner_text(self, **kwargs):
        self.inner_timeout = kwargs["timeout"]
        return self.value


class FakePage:
    def __init__(self, final_url="https://ad.domimaps.lv/908232815"):
        self.url = final_url
        self.target = None
        self.selector = None
        self.closed = False
        self.locator_value = FakeLocator()

    def goto(self, target, **kwargs):
        self.target = target
        self.goto_options = kwargs

    def locator(self, selector):
        self.selector = selector
        return self.locator_value

    def close(self):
        self.closed = True


class FakeContext:
    def __init__(self, page=None):
        self.page = page or FakePage()

    def new_page(self):
        return self.page


class FakeWorker:
    def __init__(self, value="Description", error=None):
        self.value = value
        self.error = error
        self.external_id = None

    def extract(self, external_id):
        self.external_id = external_id
        if self.error:
            raise self.error
        return self.value


class ExtractDescriptionTests(unittest.TestCase):
    def test_extracts_trimmed_description_from_fixed_advert_url(self):
        context = FakeContext()

        result = service.extract_description(context, "908232815", 1000)

        self.assertEqual(result, "Apraksts ar garumzīmēm.")
        self.assertEqual(
            context.page.target, "https://ad.domimaps.lv/908232815"
        )
        self.assertEqual(context.page.selector, "#addition-info")
        self.assertEqual(context.page.locator_value.wait["state"], "attached")
        self.assertEqual(context.page.goto_options["wait_until"], "domcontentloaded")
        self.assertTrue(context.page.closed)

    def test_rejects_unexpected_redirect(self):
        context = FakeContext(FakePage("https://example.com/elsewhere"))

        with self.assertRaisesRegex(
            service.DetailUnavailableError, "unexpected URL"
        ):
            service.extract_description(context, "908232815", 1000)

        self.assertTrue(context.page.closed)

    def test_rejects_empty_and_oversized_descriptions(self):
        for value in (" \n", "a" * (service.MAX_DESCRIPTION_BYTES + 1)):
            with self.subTest(length=len(value)):
                context = FakeContext()
                context.page.locator_value.value = value
                with self.assertRaises(service.DetailUnavailableError):
                    service.extract_description(context, "908232815", 1000)


class RequestTests(unittest.TestCase):
    def test_rejects_timeout_longer_than_go_request_budget(self):
        original = service.os.environ.get("DOMIMAPS_DETAIL_TIMEOUT_SECONDS")
        service.os.environ["DOMIMAPS_DETAIL_TIMEOUT_SECONDS"] = "21"
        try:
            with self.assertRaisesRegex(ValueError, "between 5 and 20"):
                service.Settings.from_environment()
        finally:
            if original is None:
                service.os.environ.pop("DOMIMAPS_DETAIL_TIMEOUT_SECONDS", None)
            else:
                service.os.environ["DOMIMAPS_DETAIL_TIMEOUT_SECONDS"] = original

    def test_accepts_only_numeric_external_id(self):
        with self.assertLogs("domimaps-detail", level="WARNING") as captured:
            for payload in (
                None,
                {},
                {"external_id": 908232815},
                {"external_id": "https://ad.domimaps.lv/908232815"},
                {"external_id": "123/../../example.com"},
                {"external_id": ""},
                {"external_id": "1" * 21},
            ):
                with self.subTest(payload=payload):
                    status, _ = service.process_detail_payload(payload, FakeWorker())
                    self.assertEqual(status, 400)

        output = "\n".join(captured.output)
        self.assertIn("status=400", output)
        self.assertNotIn("https://ad.domimaps.lv", output)
        self.assertNotIn("../../example.com", output)

    def test_returns_description_for_valid_request(self):
        worker = FakeWorker("A useful description")

        with self.assertLogs("domimaps-detail", level="INFO") as captured:
            status, response = service.process_detail_payload(
                {"external_id": "908232815"}, worker
            )

        self.assertEqual(status, 200)
        self.assertEqual(worker.external_id, "908232815")
        self.assertEqual(response["external_id"], "908232815")
        self.assertEqual(response["description"], "A useful description")
        output = "\n".join(captured.output)
        self.assertIn("external_id=908232815", output)
        self.assertIn("status=200", output)
        self.assertIn("duration_ms=", output)
        self.assertIn("description_bytes=20", output)

    def test_maps_browser_failures_to_retryable_response(self):
        worker = FakeWorker(
            error=service.DetailUnavailableError("challenge did not clear")
        )

        with self.assertLogs("domimaps-detail", level="WARNING") as captured:
            status, response = service.process_detail_payload(
                {"external_id": "908232815"}, worker
            )

        self.assertEqual(status, 503)
        self.assertIn("challenge", response["error"])
        output = "\n".join(captured.output)
        self.assertIn("external_id=908232815", output)
        self.assertIn("status=503", output)
        self.assertIn("duration_ms=", output)
        self.assertIn("outcome=retryable", output)
        self.assertIn("challenge did not clear", output)


if __name__ == "__main__":
    unittest.main()
