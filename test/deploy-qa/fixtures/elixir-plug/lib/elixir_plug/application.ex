defmodule ElixirPlug.Application do
  use Application

  @impl true
  def start(_type, _args) do
    port = String.to_integer(System.get_env("PORT", "4000"))

    children = [
      {Bandit, plug: ElixirPlug.Router, ip: {0, 0, 0, 0}, port: port}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ElixirPlug.Supervisor)
  end
end
