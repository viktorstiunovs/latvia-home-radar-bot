FROM mcr.microsoft.com/playwright/python:v1.60.0-noble

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir --requirement requirements.txt

COPY service.py test_service.py ./
RUN python -m unittest -v test_service.py

USER pwuser

EXPOSE 8080

CMD ["python", "service.py"]
