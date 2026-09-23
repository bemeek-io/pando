# hello-required-arg

The image bakes in a greeting at build time. The `GREETING` build argument has no default:

    docker build --build-arg GREETING="hello there" -t hello-required-arg .
    docker run -p 8080:8080 hello-required-arg
