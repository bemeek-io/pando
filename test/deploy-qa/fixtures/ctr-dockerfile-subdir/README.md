# hello-subdir

A tiny Python web server. The container build lives in `docker/`.

    docker build -f docker/Dockerfile -t hello-subdir .
    docker run -p 8000:8000 hello-subdir
