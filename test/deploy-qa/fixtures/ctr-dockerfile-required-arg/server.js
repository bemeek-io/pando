const fs = require("fs");
const http = require("http");
const greeting = fs.readFileSync(__dirname + "/GREETING", "utf8").trim();
http.createServer((req, res) => {
  res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
  res.end(`<!doctype html><title>hello</title><h1>PANDO-QA ctr-dockerfile-required-arg OK</h1><p>${greeting}</p>\n`);
}).listen(8080, "0.0.0.0", () => console.log("listening on 8080"));
