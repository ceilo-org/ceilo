export type NetRequest = {
	method: string,
	path: string,
	query: { [string]: string },
	headers: { [string]: string },
	body: string,
}

export type ServerResponse = {
	status: number,
	headers: { [string]: string },
	body: string,
}

export type Server = {
	port: number,
	close: () -> (),
}

export type Handler = (request: NetRequest, server: Server) -> string | ServerResponse

export type TLSConfiguration = {
	certfilename: string,
	keyfilename: string,
	passphrase: string?,
	cafilename: string?,
}

export type Configuration = {
	hostname: string?,
	port: number?,
	reuseport: boolean?,
	maxbodysize: number?,
	tls: TLSConfiguration?,
	handler: Handler?,
}

local function server(configuration: Configuration): Server
	error("not implemented")
end

return server
