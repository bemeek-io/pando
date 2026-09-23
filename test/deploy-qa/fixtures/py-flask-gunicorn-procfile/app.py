from flask import Flask

app = Flask(__name__)


@app.route("/")
def index():
    return "PANDO-QA py-flask-gunicorn-procfile OK\n"
