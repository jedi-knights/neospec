-- reporter.lua
-- Serializes _neospec_results and _neospec_coverage to JSON on stdout.
-- Called once after all test files have been loaded and executed.

-- Minimal JSON encoder sufficient for neospec output (no external deps).
local function json_string(s)
  s = tostring(s)
  s = s:gsub('\\', '\\\\')
  s = s:gsub('"',  '\\"')
  s = s:gsub('\n', '\\n')
  s = s:gsub('\r', '\\r')
  s = s:gsub('\t', '\\t')
  return '"' .. s .. '"'
end

local function json_value(v)
  local t = type(v)
  if t == "string"  then return json_string(v) end
  if t == "number"  then return tostring(v) end
  if t == "boolean" then return tostring(v) end
  if v == nil       then return "null" end
  if t == "table" then
    -- Detect array vs object: arrays have only integer keys starting at 1.
    local is_array = true
    local max = 0
    for k, _ in pairs(v) do
      if type(k) ~= "number" or k ~= math.floor(k) or k < 1 then
        is_array = false
        break
      end
      if k > max then max = k end
    end
    if is_array and max == #v then
      local parts = {}
      for _, item in ipairs(v) do
        table.insert(parts, json_value(item))
      end
      return "[" .. table.concat(parts, ",") .. "]"
    else
      local parts = {}
      for k, val in pairs(v) do
        table.insert(parts, json_string(tostring(k)) .. ":" .. json_value(val))
      end
      return "{" .. table.concat(parts, ",") .. "}"
    end
  end
  return "null"
end

function _neospec_report()
  -- Build the coverage array from _neospec_coverage.
  local coverage = {}
  for path, lines in pairs(_neospec_coverage or {}) do
    table.insert(coverage, { path = path, lines = lines })
  end

  local output = {
    tests    = _neospec_results or {},
    coverage = coverage,
  }

  io.write(json_value(output) .. "\n")
  io.flush()
end
