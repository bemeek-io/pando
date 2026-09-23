const http = require("http");
const required = ["SECRET_KEY", "SITE_NAME"];
const missing = required.filter((k) => !process.env[k]);
if (missing.length) {
  console.error(`Missing required environment variables: ${missing.join(", ")}. Copy .env.example to .env and fill them in.`);
  process.exit(1);
}
const port = Number(process.env.PORT || 3000);
http.createServer((req, res) => {
  res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
  res.end(`<!doctype html><title>${process.env.SITE_NAME}</title><h1>PANDO-QA compose-env-file OK</h1><p>site: ${process.env.SITE_NAME}</p>\n`);
}).listen(port, "0.0.0.0", () => console.log(`listening on ${port}`));
