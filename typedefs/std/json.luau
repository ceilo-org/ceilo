local json = {}

--- Decodes a JSON string into Luau values (tables, strings, numbers, booleans, or nil).
function json.deserialize(value: string): any
	error("not implemented")
end

--- Encodes a Luau value as a JSON string. When `pretty` is true, formats the output with indentation.
function json.serialize(value: any, pretty: boolean?): string
	error("not implemented")
end

return json
