const port = Number(Deno.env.get("PORT") ?? 8000);

Deno.serve({ port, hostname: "0.0.0.0" }, (_req) =>
  new Response("<h1>PANDO-QA js-deno OK</h1>", {
    headers: { "content-type": "text/html" },
  }));
