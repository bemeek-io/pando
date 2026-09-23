# Build from the repository root:
#   docker build -f docker/Dockerfile .
FROM python:3.12-alpine
WORKDIR /srv
COPY app/ ./app/
EXPOSE 8000
CMD ["python", "-u", "app/main.py"]
