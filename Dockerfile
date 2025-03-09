FROM ghcr.io/astral-sh/uv:debian-slim


ENV UV_COMPILE_BYTECODE=1
EXPOSE 8501

RUN mkdir -p /app
WORKDIR /app

COPY pyproject.toml uv.lock README.md ./
COPY src/ ./src/
COPY dashboards/ ./dashboards/

RUN uv sync --group dashboards --frozen

HEALTHCHECK CMD curl --fail http://localhost:8501/_stcore/health

WORKDIR /app/dashboards
CMD ["uv", "run", "streamlit", "run", "balances.py", "--server.port=8501", "--server.address=0.0.0.0"]

