const http = require("http");
const port = Number(process.env.PORT || 8080);
http.createServer((req, res) => {
  res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
  res.end("<!doctype html><title>hello</title><h1>PANDO-QA ctr-dockerfile-expose OK</h1>\n");
}).listen(port, "0.0.0.0", () => console.log(`listening on ${port}`));
