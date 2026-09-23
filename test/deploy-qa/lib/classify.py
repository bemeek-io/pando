"""Assign every result a language, packaging and (for failures) exactly one cause.

The rules below match log signatures. OVERRIDE holds causes established by
reading the logs of the 2026-09-22 run (issue #55); as fixes land those apps
should start passing, and any that still fail for a new reason fall through to
the rules or to "other". Review "other" rows after every run and add a rule or
an override, so every failure keeps exactly one cause.
"""
import json

LANG_BY_PREFIX = [("static-", "HTML / static"), ("js-", "JavaScript / TypeScript"), ("py-", "Python"),
                  ("go-", "Go"), ("java-", "Java"), ("rust-", "Rust"), ("ruby-", "Ruby"), ("php-", "PHP"),
                  ("dotnet-", ".NET"), ("elixir-", "Elixir")]
LANG_BY_ID = {  # generated container/topology fixtures: the language inside the package
    "ctr-dockerfile-expose": "JavaScript / TypeScript", "ctr-dockerfile-no-expose": "Python",
    "ctr-dockerfile-subdir": "Go", "ctr-containerfile": "Go", "ctr-multistage-go": "Go",
    "ctr-dockerfile-prod-only": "JavaScript / TypeScript", "ctr-dockerfile-buildarg": "Go",
    "ctr-dockerfile-required-arg": "Go", "ctr-nginx-static": "HTML / static",
    "ctr-dockerfile-env-required": "Python", "compose-web-redis": "Python", "compose-full-stack": "Multiple",
    "compose-images-only": "Multiple", "compose-named-volume": "Python", "compose-env-file": "JavaScript / TypeScript",
    "compose-profiles": "Python", "compose-worker-queue": "Python", "edge-makefile-c": "C",
    "edge-prebuilt-binary": "Go", "edge-procfile-web-worker": "Python", "edge-monorepo-no-compose": "Multiple",
    "edge-readme-only": "None", "edge-library": "Python", "edge-cli-tool": "Go",
}
LANG_NORMAL = {"javascript": "JavaScript / TypeScript", "typescript": "JavaScript / TypeScript",
               "html": "HTML / static", "python": "Python", "go": "Go", "java": "Java", "rust": "Rust",
               "ruby": "Ruby", "php": "PHP", "csharp": ".NET", "polyglot": "Multiple", "c": "C", "none": "None"}

PACK_BY_ID_PREFIX = [("ctr-", "Dockerfile"), ("compose-", "Compose"), ("static-", "Static site")]
PACK_NORMAL = {"static": "Static site", "static-generator": "Static site", "dockerfile": "Dockerfile",
               "compose": "Compose", "buildpack": "No build files (language detection)", "image": "Published image",
               "library": "Should be refused", "empty": "Should be refused"}
PACK_BY_ID = {"edge-makefile-c": "No build files (language detection)", "edge-prebuilt-binary": "No build files (language detection)",
              "edge-procfile-web-worker": "No build files (language detection)", "edge-monorepo-no-compose": "Should be refused",
              "edge-readme-only": "Should be refused", "edge-library": "Should be refused", "edge-cli-tool": "Should be refused"}


def language(r):
    i = r["id"]
    if i in LANG_BY_ID:
        return LANG_BY_ID[i]
    for p, l in LANG_BY_PREFIX:
        if i.startswith(p):
            return l
    return LANG_NORMAL.get((r.get("language") or "").lower(), r.get("language") or "?")


def packaging(r):
    i = r["id"]
    if i in PACK_BY_ID:
        return PACK_BY_ID[i]
    for p, k in PACK_BY_ID_PREFIX:
        if i.startswith(p):
            return k
    if r.get("source") == "generated":
        return "No build files (language detection)"
    return PACK_NORMAL.get((r.get("packaging") or "").lower(), r.get("packaging") or "?")


SOURCE = {"generated": "Generated apps", "public-github": "Public GitHub repos", "docker-image": "Published Docker images"}

# Cause labels. PANDO = counts against Pando; OTHER = not Pando's fault.
C = {
    "static_nginx": ("PANDO", "Static-site builds write an nginx config nginx rejects"),
    "node18": ("PANDO", "Buildpack builds use Node 18; current Vite/Next/Angular/Nest need Node 20+"),
    "toolchain": ("PANDO", "Other buildpack toolchain problems (Gradle 9, Ruby/PHP/Go/Java versions, Bun)"),
    "start_ignored": ("PANDO", "Answered start command is not passed to the build"),
    "no_workloads": ("PANDO", "Accept refused with 'no workloads' after answering questions"),
    "no_detector": ("PANDO", "No detector for the language or layout"),
    "static_as_node": ("PANDO", "Static-build site treated as a Node server (asks for a start command)"),
    "wrong_port": ("PANDO", "Port assumed from framework default, app listens elsewhere"),
    "localhost": ("PANDO", "App listens only on localhost inside the container"),
    "port_env": ("PANDO", "PORT environment variable never set"),
    "compose": ("PANDO", "Compose topology problems (DNS names, service names, command quoting, primary port)"),
    "wrong_start": ("PANDO", "Wrong start command chosen"),
    "env_missing": ("PANDO", "Required environment variables not detected or asked for"),
    "false_positive": ("PANDO", "Library or CLI tool deployed as a web app instead of refused"),
    "build_arg": ("PANDO", "Required build argument not detected"),
    "workspace": ("PANDO", "Subdirectory of a Cargo workspace cannot build on its own"),
    "arm64": ("PANDO", "No arm64 build of the image or base image"),
    "upstream": ("OTHER", "Upstream repository broken or outdated"),
    "needs_config": ("OTHER", "App needs configuration or services nobody supplied"),
    "blocked_policy": ("OTHER", "Blocked on purpose by Pando's safety rules"),
    "harness": ("OTHER", "Test harness or environment"),
    "other": ("PANDO", "Other"),
}

# Per-case causes established by reading the no-AI logs (see the report's evidence column).
OVERRIDE = {
    "ctr-containerfile": "no_workloads", "ctr-dockerfile-subdir": "no_workloads", "gh-nuxt-starter": "no_workloads",
    "gh-spring-petclinic": "no_workloads", "static-docs-dir": "no_workloads",
    "gh-spring-gs-rest-service": "toolchain", "ruby-sinatra": "toolchain", "gh-heroku-ruby": "toolchain",
    "java-spring-maven": "toolchain", "gh-heroku-go": "toolchain", "js-bun": "toolchain",
    "gh-laravel": "toolchain",
    "ac-angular": "upstream", "ac-apache-php": "upstream", "ac-react-express-mongodb": "upstream",
    "ac-react-rust-postgres": "upstream", "ac-vuejs": "upstream", "gh-angular-realworld": "upstream",
    "gh-docker-getting-started": "upstream", "gh-node-realworld": "upstream",
    "gh-axum-hello-world": "workspace", "ctr-dockerfile-required-arg": "build_arg",
    "gh-httpbin": "arm64", "gh-node-docker-good-defaults": "arm64",
    "gh-vite-template-vanilla": "static_as_node", "js-astro-static": "static_as_node",
    "ac-traefik-golang": "blocked_policy", "gh-full-stack-fastapi": "blocked_policy",
    "gh-heroku-node": "wrong_port", "js-express-no-start": "wrong_port", "js-koa": "wrong_port",
    "js-procfile": "wrong_port", "rust-axum": "wrong_port", "php-composer-slim": "wrong_port",
    "js-localhost-trap": "localhost", "py-django": "localhost", "gh-heroku-python": "localhost",
    "py-fastapi": "localhost", "py-uv": "localhost", "gh-svelte-template": "localhost",
    "py-flask-gunicorn-procfile": "port_env",
    "compose-env-file": "env_missing", "ctr-dockerfile-env-required": "env_missing",
    "img-dozzle": "needs_config", "img-ghost": "needs_config", "img-miniflux": "needs_config",
    "img-vaultwarden": "needs_config", "gh-vaultwarden": "needs_config", "ac-wordpress-mysql": "needs_config",
    "gh-gin-realworld": "needs_config", "gh-hackathon-starter": "needs_config",
    "gh-miniflux": "wrong_start", "gh-heroku-php": "wrong_start", "gh-heroku-java": "wrong_port", "ac-fastapi": "wrong_port", "gh-gatsby-starter-blog": "wrong_start",
}
COMPOSE_IDS = {"compose-web-redis", "compose-worker-queue", "ac-nginx-golang-mysql", "ac-nginx-flask-mysql",
               "ac-nginx-nodejs-redis", "ac-nginx-flask-mongo", "ac-react-express-mysql", "ac-react-java-mysql",
               "ac-spring-postgres", "gh-example-voting-app", "ac-gitea-postgres"}


def cause(r):
    if r["outcome"].startswith("PASS"):
        return None
    blob = json.dumps(r)
    i = r["id"]
    if "signal 9 (Killed)" in blob or "lease does not exist" in blob:
        return "harness"
    if r["outcome"] == "HARNESS_ERROR" or "No such image" in blob or (
            "could not fetch this app" in blob and "deadline" in blob):
        return "harness"
    if r["outcome"] == "FAIL_FALSE_POSITIVE":
        return "false_positive"
    if "emerg] unknown directive" in blob:
        return "static_nginx"
    if "EBADENGINE" in blob and r["outcome"] == "FAIL_DEPLOY":
        return "node18"
    if "No start command could be found" in blob:
        return "start_ignored"
    if i in OVERRIDE:
        return OVERRIDE[i]
    if i in COMPOSE_IDS:
        return "compose"
    if "VALID_PRIMARY_WORKLOAD" in blob:
        return "no_workloads"
    if r["outcome"] == "FAIL_DETECT" and any(q.get("key") == "build_method" for q in
                                               ((r.get("detection_after_answers") or r.get("detection") or {}).get("questions") or [])):
        return "no_detector"
    if r["outcome"] == "FAIL_DETECT" and r.get("detection_final_status") == "needs_answers":
        # With AI, screening answers build_method in prose; Pando still cannot build it.
        return "no_detector"
    return "other"
