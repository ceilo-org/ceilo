local bcrypt = {}

--- Takes a string password and number cost, and returns its bcrypt hash.
function bcrypt.hash(password: string, cost: number?): string
    error("not implemented")
end

--- Compares a raw password against the stored bcrypt hash..
function bcrypt.compare(rawPassword: string, hashedPassword: string): boolean
    error("not implemented")
end

return bcrypt
