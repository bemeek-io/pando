from django.http import HttpResponse


def index(request):
    return HttpResponse("PANDO-QA py-django OK\n", content_type="text/plain")
