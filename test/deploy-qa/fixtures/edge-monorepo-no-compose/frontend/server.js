const fs = require("fs");
const http = require("http");
const path = require("path");
const port = Number(process.env.PORT || 3000);
const root = path.join(__dirname, "public");
http.createServer((req, res) => {
  const rel = req.url === "/" ? "index.html" : req.url.replace(/^\/+/, "").split("?")[0];
  const file = path.join(root, path.normalize(rel));
  if (!file.startsWith(root)) { res.writeHead(403); return res.end(); }
  fs.readFile(file, (err, data) => {
    if (err) { res.writeHead(404); return res.end("not found\n"); }
    const type = file.endsWith(".html") ? "text/html; charset=utf-8" : "application/octet-stream";
    res.writeHead(200, { "Content-Type": type });
    res.end(data);
  });
}).listen(port, "0.0.0.0", () => console.log(`frontend on ${port}`));
