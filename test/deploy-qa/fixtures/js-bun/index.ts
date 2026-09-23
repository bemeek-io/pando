const port = Number(process.env.PORT ?? 3000);

const server = Bun.serve({
  port,
  hostname: "0.0.0.0",
  fetch(req) {
    return new Response("<h1>PANDO-QA js-bun OK</h1>", {
      headers: { "content-type": "text/html" },
    });
  },
});

console.log(`listening on ${server.port}`);
