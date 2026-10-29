local client = require("@ceilo/net/client")

export type Metadata = client.Metadata
export type NetResponse = client.NetResponse
export type Client = client.Client

local net = {}

net.client = client

return net
