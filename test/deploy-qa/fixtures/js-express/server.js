const express = require('express');
const app = express();
const port = process.env.PORT || 3000;
app.get('/', (req, res) => res.send('<h1>PANDO-QA js-express OK</h1>'));
app.get('/healthz', (req, res) => res.json({ ok: true }));
app.listen(port, () => console.log(`listening on ${port}`));
