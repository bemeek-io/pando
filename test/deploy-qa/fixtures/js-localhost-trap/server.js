const express = require('express');
const app = express();
const port = process.env.PORT || 3000;
app.get('/', (req, res) => res.send('<h1>PANDO-QA js-localhost-trap OK</h1>'));
app.listen(port, '127.0.0.1', () => console.log(`listening on http://127.0.0.1:${port}`));
