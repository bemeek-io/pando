const http = require("http");
const token = process.env.API_TOKEN;
if (!token) {
  console.error("API_TOKEN is not set. This service needs API_TOKEN to call its upstream API; set it in the environment and restart.");
  process.exit(1);
}
http.createServer((req, res) => {
  res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
  res.end(`<!doctype html><title>hello</title><h1>PANDO-QA ctr-dockerfile-env-required OK</h1><p>token length ${token.length}</p>\n`);
}).listen(8080, "0.0.0.0", () => console.log("listening on 8080"));
