import { createServer } from 'node:http';
const port = Number(process.env.PORT ?? 3000);
createServer((req, res) => {
  res.writeHead(200, { 'content-type': 'text/html' });
  res.end('<h1>PANDO-QA js-node-http-esm OK</h1>');
}).listen(port, '0.0.0.0', () => console.log(`listening on ${port}`));
