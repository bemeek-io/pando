require "sinatra/base"

class App < Sinatra::Base
  set :host_authorization, { permitted_hosts: [] }

  get "/" do
    content_type "text/plain"
    "PANDO-QA ruby-sinatra OK\n"
  end
end
