const write = print

const INDENT = "  "
const MAX_DEPTH = 8
const ESC = "\27["
const BLUE = ESC .. "34m"
const RESET = ESC .. "0m"
const USE_COLOR = true

local function paint(s: string): string
    return if USE_COLOR then BLUE .. s .. RESET else s
end

const OPEN = paint("{")
const CLOSE = paint("}")
const EMPTY = paint("{}")
const TRUNCATED = OPEN .. "..." .. CLOSE

local function formatKey(k: any): string
    if type(k) == "string" then
        if string.match(k, "^[%a_][%w_]*$") then
            return k
        end
        return `[{string.format("%q", k)}]`
    end
    return `[{tostring(k)}]`
end

local function formatValue(v: any): string
    if type(v) == "string" then
        return string.format("%q", v)
    end
    return tostring(v)
end

local function sortKeys(a: any, b: any): boolean
    local ta, tb = type(a), type(b)
    if ta ~= tb then
        return ta < tb -- "number" sorts before "string"
    end
    if ta == "number" or ta == "string" then
        return a < b
    end
    return tostring(a) < tostring(b)
end

local function serialize(t: { [any]: any }, depth: number, seen: { [any]: boolean }): string
    -- Respect custom __tostring
    local mt: any = getmetatable(t)
    if type(mt) == "table" and rawget(mt :: any, "__tostring") ~= nil then
        return tostring(t)
    end

    if seen[t] then
        return "<cycle>"
    end
    if next(t) == nil then
        return EMPTY
    end
    if depth >= MAX_DEPTH then
        return TRUNCATED
    end

    seen[t] = true

    local keys = {}
    for k in t do
        table.insert(keys, k)
    end
    table.sort(keys, sortKeys)

    local pad = string.rep(INDENT, depth + 1)
    local lines = { OPEN }

    for _, k in keys do
        local v = t[k]
        local rendered = if type(v) == "table"
            then serialize(v, depth + 1, seen)
            else formatValue(v)
        table.insert(lines, pad .. formatKey(k) .. " = " .. rendered .. ",")
    end

    table.insert(lines, string.rep(INDENT, depth) .. CLOSE)

    seen[t] = nil
    return table.concat(lines, "\n")
end

_G.print = function(...: any)
    if select("#", ...) == 1 and type((...)) == "table" then
        write(serialize((...), 0, {}))
    else
        write(...)
    end
end