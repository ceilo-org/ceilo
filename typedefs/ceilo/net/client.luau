export type Metadata = {
	method: string?,
	body: string?,
	headers: { [string]: string }?,
}

export type NetResponse = {
	body: string,
	headers: { [string]: string },
	status: number,
	ok: boolean,
}

export type Client = {
	request: (url: string, metadata: Metadata?) -> NetResponse,
}

local client = {}
--- Sends an HTTP request to the URL and returns the server's response.
function client.request(url: string, metadata: Metadata?): NetResponse
	error("not implemented")
end

return client
