const fs = require("fs");
const http = require("http");
const version = fs.readFileSync(__dirname + "/VERSION", "utf8").trim();
http.createServer((req, res) => {
  res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
  res.end(`<!doctype html><title>hello</title><h1>PANDO-QA ctr-dockerfile-buildarg OK</h1><p>version ${version}</p>\n`);
}).listen(8080, "0.0.0.0", () => console.log(`version ${version} listening on 8080`));
