local ceiloClient = require("@ceilo/net/client")

export type Metadata = ceiloClient.Metadata
export type NetResponse = ceiloClient.NetResponse
export type Client = ceiloClient.Client

local client = {}

client.request = ceiloClient.request

return client
