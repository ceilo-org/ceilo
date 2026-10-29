local server = require("@ceilo/net/server")
local client = require("@ceilo/net/client")

export type NetRequest = server.NetRequest
export type ServerResponse = server.ServerResponse
export type Handler = server.Handler
export type TLSConfiguration = server.TLSConfiguration
export type Configuration = server.Configuration
export type Server = server.Server

export type Metadata = client.Metadata
export type NetResponse = client.NetResponse
export type Client = client.Client

local net = {}

net.server = server
net.client = client

return net
