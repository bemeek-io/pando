const http = require("http");
http.createServer((req, res) => {
  res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
  res.end("<!doctype html><title>hello</title><h1>PANDO-QA ctr-dockerfile-no-expose OK</h1>\n");
}).listen(5000, "0.0.0.0", () => console.log("listening on 5000"));
