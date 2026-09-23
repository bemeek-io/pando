# hello-env-required

Requires `API_TOKEN` at runtime:

    docker build -t hello-env .
    docker run -e API_TOKEN=... -p 8080:8080 hello-env
