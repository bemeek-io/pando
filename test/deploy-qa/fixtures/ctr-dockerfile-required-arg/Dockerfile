FROM node:22-alpine
# Required: docker build --build-arg GREETING="hello there" .
ARG GREETING
WORKDIR /app
COPY package.json server.js ./
RUN if [ -z "${GREETING}" ]; then \
      echo "The GREETING build argument is required. Build with: docker build --build-arg GREETING='hello' ." >&2; \
      exit 1; \
    fi; \
    echo "${GREETING}" > GREETING
EXPOSE 8080
CMD ["node", "server.js"]
