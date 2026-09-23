FROM node:22-alpine
ARG APP_VERSION=1.0.0
WORKDIR /app
COPY package.json server.js ./
RUN echo "${APP_VERSION}" > VERSION
EXPOSE 8080
CMD ["node", "server.js"]
