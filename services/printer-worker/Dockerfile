FROM --platform=linux/amd64 python:3.11-slim

RUN groupadd --system appgroup && useradd --system --gid appgroup appuser

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app/ ./app/

USER appuser

CMD ["python", "-m", "app.main"]
