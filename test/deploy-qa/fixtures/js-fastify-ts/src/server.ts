import Fastify from 'fastify';

const app = Fastify({ logger: true });
const port = Number(process.env.PORT ?? 3000);

app.get('/', async (_req, reply) => {
  reply.type('text/html');
  return '<h1>PANDO-QA js-fastify-ts OK</h1>';
});

app.listen({ port, host: '0.0.0.0' }).catch((err) => {
  app.log.error(err);
  process.exit(1);
});
