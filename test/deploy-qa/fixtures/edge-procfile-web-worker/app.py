from flask import Flask

app = Flask(__name__)


@app.get("/")
def index():
    return "<!doctype html><title>hello</title><h1>PANDO-QA edge-procfile-web-worker OK</h1>\n"


@app.get("/healthz")
def healthz():
    return {"status": "ok"}
