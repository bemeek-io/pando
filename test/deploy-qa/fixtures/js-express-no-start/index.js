const express = require('express');
const app = express();
app.get('/', (req, res) => res.send('<h1>PANDO-QA js-express-no-start OK</h1>'));
app.listen(8080, () => console.log('listening on 8080'));
