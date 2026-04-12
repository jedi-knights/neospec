-- reporter.lua
-- Serializes _neospec_results and _neospec_coverage to JSON on stdout.
-- Called once after all test files have been loaded and executed.

-- Guard against double-load: a second source of this file would redefine
-- _neospec_report and, if called again, emit a second JSON payload to stdout.
-- Two JSON objects on stdout would corrupt the stream the Go consumer parses.
if _neospec_report_loaded then
	return
end
_neospec_report_loaded = true

-- Minimal JSON encoder sufficient for neospec output (no external deps).
local function json_string(s)
	if s == nil then
		error("json_string: nil is not a valid JSON string value", 2)
	end
	s = tostring(s)
	-- Backslash MUST be escaped first. It is the JSON escape prefix and will be
	-- introduced by every subsequent gsub below. Reordering this step causes
	-- all newly-added backslashes to be double-escaped on a second pass
	-- (e.g. the literal `"` becomes `\\"` instead of `\"`).
	s = s:gsub("\\", "\\\\")
	s = s:gsub('"', '\\"')
	s = s:gsub("\n", "\\n")
	s = s:gsub("\r", "\\r")
	s = s:gsub("\t", "\\t")
	-- Escape remaining control characters (U+0000–U+001F) that the JSON spec
	-- requires to be escaped. Test error messages may embed ANSI sequences
	-- (e.g. \27[...); an unescaped ESC makes the output invalid JSON.
	--
	-- U+0000 (NUL) is handled separately via %z. In LuaJIT (the engine Neovim
	-- embeds), a NUL byte inside a character-class bracket terminates the pattern
	-- string early (patterns are processed as C strings internally), so
	-- [\0-\31] silently matches nothing for NUL. %z is the portable Lua pattern
	-- class for NUL and is safe across all Lua implementations.
	s = s:gsub("%z", "\\u0000")
	s = s:gsub("[\1-\31]", function(c)
		return string.format("\\u%04x", string.byte(c))
	end)
	return '"' .. s .. '"'
end

local function json_value(v, depth)
	depth = depth or 0
	-- Abort at depth 64 to prevent stack overflow from deeply nested or
	-- self-referential tables. Return a safe string sentinel rather than nil
	-- so the containing object remains valid JSON.
	if depth > 64 then
		return '"[max depth exceeded]"'
	end
	local t = type(v)
	if t == "string" then
		return json_string(v)
	end
	if t == "number" then
		-- Guard against non-finite values that produce invalid JSON literals.
		-- tostring(math.huge) = "inf", tostring(0/0) = "-nan" — both are rejected
		-- by strict JSON parsers. Emit null instead so the output remains valid.
		if v ~= v or v == math.huge or v == -math.huge then -- v ~= v is the IEEE 754 NaN check
			return "null"
		end
		return tostring(v)
	end
	if t == "boolean" then
		return tostring(v)
	end
	if v == nil then
		return "null"
	end
	if t == "table" then
		-- Detect array vs object: arrays have only contiguous integer keys starting
		-- at 1. Sparse tables (e.g. {[1]=a, [3]=b}) fail the max==count check and
		-- fall through to the object encoder — this is intentional and correct for
		-- coverage line maps, which are always keyed by line number and encoded as
		-- JSON objects.
		--
		-- The # operator is intentionally NOT used here: its result is undefined on
		-- non-contiguous (holey) integer tables and can silently drop entries when
		-- the last contiguous boundary is less than the actual maximum key.
		local is_array = true
		local max = 0
		local count = 0
		for k, _ in pairs(v) do
			-- Type/value guard runs before count and max are updated so that
			-- a non-integer key does not inflate count or set a stale max value
			-- before the break. Although is_array=false short-circuits the
			-- max==count check today, stale locals are a maintenance trap.
			if type(k) ~= "number" or k ~= math.floor(k) or k < 1 then
				is_array = false
				break
			end
			count = count + 1
			if k > max then
				max = k
			end
		end
		if is_array and max == count then
			local parts = {}
			for _, item in ipairs(v) do
				table.insert(parts, json_value(item, depth + 1))
			end
			return "[" .. table.concat(parts, ",") .. "]"
		else
			-- Sort keys for deterministic output: pairs() does not guarantee
			-- stable iteration order, making unsorted JSON non-reproducible
			-- across runs and harder to diff or snapshot-test.
			local keys = {}
			for k in pairs(v) do
				keys[#keys + 1] = k
			end
			table.sort(keys, function(a, b) return tostring(a) < tostring(b) end)
			local parts = {}
			for _, k in ipairs(keys) do
				table.insert(parts, json_string(tostring(k)) .. ":" .. json_value(v[k], depth + 1))
			end
			return "{" .. table.concat(parts, ",") .. "}"
		end
	end
	return "null"
end

-- Call-once guard: prevents duplicate JSON output if _neospec_report() is
-- somehow invoked more than once (e.g. from a user init_file that also calls
-- it, or a future shim bug). Two JSON objects on stdout would corrupt the
-- stream the Go consumer parses.
local _report_emitted = false

function _neospec_report()
	if _report_emitted then
		return
	end
	_report_emitted = true

	-- Wrap the serialization in pcall so that corrupted globals
	-- (_neospec_results or _neospec_coverage set to a non-table value by a
	-- misbehaving test) cannot crash the function and leave the Go consumer
	-- with no output. On failure, emit a minimal error JSON so the consumer
	-- sees a structured diagnostic rather than an "unexpected EOF".
	local ok, err = pcall(function()
		-- Build the coverage array from _neospec_coverage.
		-- Skip entries with no line data: an empty Lua table encodes as "[]" (a JSON
		-- array) rather than "{}" (a JSON object), which would confuse consumers that
		-- expect a map. Files with zero recorded hits carry no useful information.
		local coverage = {}
		for path, lines in pairs(_neospec_coverage or {}) do
			if next(lines) ~= nil then
				table.insert(coverage, { path = path, lines = lines })
			end
		end

		local output = {
			tests = _neospec_results or {},
			coverage = coverage,
		}

		-- Write to stdout. The Go consumer parses the entire stdout as JSON, so
		-- stdout must contain only this line. Calls to print() from test files
		-- also write to stdout in headless mode and will corrupt the output.
		-- Print capture (redirecting print to output_buf) is a planned future feature.
		io.write(json_value(output) .. "\n")
		io.flush()
	end)
	if not ok then
		io.write('{"tests":[],"coverage":[],"error":' .. json_string(tostring(err)) .. "}\n")
		io.flush()
	end
end
