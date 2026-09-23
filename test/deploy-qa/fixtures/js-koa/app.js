const Koa = require('koa');
const Router = require('@koa/router');
const app = new Koa();
const router = new Router();
router.get('/', (ctx) => { ctx.type = 'html'; ctx.body = '<h1>PANDO-QA js-koa OK</h1>'; });
app.use(router.routes()).use(router.allowedMethods());
const port = process.env.PORT || 4000;
app.listen(port, () => console.log(`koa listening on ${port}`));
