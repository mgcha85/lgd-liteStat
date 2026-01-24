FROM python:3.13-slim

WORKDIR /app

# Install system dependencies for psycopg2
RUN apt-get update && apt-get install -y libpq-dev gcc && rm -rf /var/lib/apt/lists/*

# Copy requirements first to leverage cache
COPY requirements.txt .

# Argument for custom PyPI mirror (Useful for offline/intranet environments)
ARG PIP_INDEX_URL
ARG PIP_TRUSTED_HOST

# Create pip.conf if arguments are provided
RUN if [ -n "$PIP_INDEX_URL" ]; then \
    echo "[global]" > /etc/pip.conf && \
    echo "index-url = $PIP_INDEX_URL" >> /etc/pip.conf && \
    echo "trusted-host = $PIP_TRUSTED_HOST" >> /etc/pip.conf; \
    fi

RUN pip install --no-cache-dir -r requirements.txt

# Copy source code
COPY . .

# Environment variables will be loaded from .env file at runtime
CMD ["python", "main.py"]
