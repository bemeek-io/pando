var builder = WebApplication.CreateBuilder(args);
var app = builder.Build();

app.MapGet("/", () => "PANDO-QA dotnet-minimal OK\n");

app.Run();
