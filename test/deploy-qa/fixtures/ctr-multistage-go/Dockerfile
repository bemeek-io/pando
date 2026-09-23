FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hello .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hello /hello
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/hello"]
