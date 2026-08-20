-- coverage_hook.lua
-- Installs a debug.sethook listener that records every executed Lua line.
-- Coverage data is accumulated in _neospec_coverage, keyed by source file path.
-- This file must be loaded BEFORE any code under test is required.

-- Guard against double-load: a second source of this file would reset
-- _neospec_coverage to {} and discard all data accumulated by the first hook.
if _neospec_coverage_loaded == true then
	return
end
_neospec_coverage_loaded = true

_neospec_coverage = _neospec_coverage or {}

-- _neospec_is_test_source returns true for source files that belong to the
-- project under test (i.e. not the harness or hook itself).
local function is_project_source(source)
	-- Lua debug info prefixes sources with '@' for file paths.
	if source:sub(1, 1) ~= "@" then
		return false
	end
	local path = source:sub(2)
	-- Exclude neospec's own shim/harness files. These patterns match by filename
	-- suffix only — any project file whose name ends with one of these suffixes
	-- (e.g. "test_harness.lua", "my_reporter.lua") will also be excluded from
	-- coverage. This is a known limitation: anchoring to a neospec-specific path
	-- prefix would require knowing the install location at runtime. In practice
	-- the neospec_ prefix on the shim and the specificity of "coverage_hook",
	-- "harness", and "reporter" make false exclusions unlikely. Users should
	-- avoid naming test helpers with these exact suffixes if they want them
	-- included in coverage reports.
	if path:find("neospec_run%.lua$") then
		return false
	end
	if path:find("coverage_hook%.lua$") then
		return false
	end
	if path:find("harness%.lua$") then
		return false
	end
	if path:find("reporter%.lua$") then
		return false
	end
	-- When _neospec_coverage_include is set, only record files whose path
	-- contains at least one of the listed substrings. This lets callers
	-- restrict coverage to their plugin's own source tree and exclude
	-- Neovim's internal runtime files.
	if type(_neospec_coverage_include) == "table" and #_neospec_coverage_include > 0 then
		for _, pattern in ipairs(_neospec_coverage_include) do
			if path:find(pattern, 1, true) then
				return true
			end
		end
		return false
	end
	return true
end

local function hook(event)
	-- The hook is registered for "l" (line) events only. Explicitly guard
	-- against other event types so that a future registration change (e.g.
	-- adding call/return events) does not silently inflate coverage counts by
	-- recording the currentline of call/return frames as executed lines.
	if event ~= "line" then
		return
	end
	-- Level 2 is the function whose line triggered the event (level 1 is this
	-- hook). This is the correct level for a "l" (line) event hook.
	local info = debug.getinfo(2, "Sl")
	if not info then
		return
	end
	local source = info.source or ""
	if not is_project_source(source) then
		return
	end
	local path = source:sub(2)
	-- Guard against a bare "@" source (path would be empty string), which would
	-- create an empty-string key in _neospec_coverage and produce a coverage
	-- entry with path = "" in the JSON output.
	if #path == 0 then
		return
	end
	local line = info.currentline
	if line < 0 then
		return
	end

	-- Guard against _neospec_coverage being set to a non-table value by a
	-- misbehaving test (e.g. `_neospec_coverage = nil`). Without this check,
	-- indexing a nil or non-table value raises "attempt to index a nil value"
	-- and aborts every subsequent hook call, silently discarding all coverage
	-- data for the remainder of the run.
	if type(_neospec_coverage) ~= "table" then
		_neospec_coverage = {}
	end
	if not _neospec_coverage[path] then
		_neospec_coverage[path] = {}
	end
	_neospec_coverage[path][line] = (_neospec_coverage[path][line] or 0) + 1
end

-- ---------------------------------------------------------------------------
-- Executable-line discovery
-- ---------------------------------------------------------------------------
--
-- The line hook above can only record lines that RAN. Without a second source
-- of truth for which lines COULD have run, every recorded line has at least
-- one hit and coverage is 100% by construction -- the metric cannot detect
-- untested code, which is the only thing it exists to do.
--
-- debug.getinfo(fn, "L").activelines is the obvious candidate but only covers
-- the top-level chunk: a function body is a separate prototype whose lines are
-- absent. Instead we walk the prototype tree via LuaJIT's jit.util, mapping
-- every bytecode position to its source line. That yields the complete
-- executable-line set, including bodies of functions no test ever called.

-- Bound the prototype recursion. Deeply nested closures are legitimate, but an
-- unbounded walk on a malformed or adversarial chunk must not blow the stack.
local MAX_PROTO_DEPTH = 100

local function collect_proto_lines(proto, acc, depth, util)
	if depth > MAX_PROTO_DEPTH then
		return acc
	end

	-- Walk bytecode positions; funcinfo(proto, pc) reports the source line for
	-- that instruction. funcbc returns nil once pc runs past the last opcode.
	local pc = 0
	while true do
		local ok, bc = pcall(util.funcbc, proto, pc)
		if not ok or bc == nil then
			break
		end
		local ok_info, info = pcall(util.funcinfo, proto, pc)
		if ok_info and info and info.currentline and info.currentline > 0 then
			acc[info.currentline] = true
		end
		pc = pc + 1
	end

	-- Child prototypes live among the GC constants at negative indices.
	local i = -1
	while true do
		local ok, k = pcall(util.funck, proto, i)
		if not ok or k == nil then
			break
		end
		if type(k) == "proto" then
			collect_proto_lines(k, acc, depth + 1, util)
		end
		i = i - 1
	end

	return acc
end

-- ---------------------------------------------------------------------------
-- Function discovery
-- ---------------------------------------------------------------------------
--
-- lcov carries FN/FNDA/FNF/FNH records alongside line data; without them the
-- function column of any lcov viewer reads empty. Each Lua prototype IS a
-- function, so the same walk that yields executable lines also enumerates
-- every function including nested closures.
--
-- Two things the runtime does not give us:
--
--   Names. A prototype does not store one -- in Lua a function's name comes
--   from the enclosing assignment, and debug.getinfo only recovers it for a
--   live call frame. We read the source line at linedefined and match the
--   common definition forms. Anonymous callbacks fall back to a positional
--   label, which is honest rather than wrong.
--
--   Call counts. The hook records line hits, not invocations. We use the hit
--   count of the function's first body line, which equals the call count for
--   any function whose first statement is unconditional -- nearly all of them.
--   The accurate alternative is a "c" (call) debug hook, which would fire on
--   every call in the suite including Neovim's own internals; that overhead is
--   not worth the precision for a coverage report.

_neospec_functions = _neospec_functions or {}

--- Read a source file into a line-indexed table, or nil if unreadable.
local function read_source_lines(path)
	local fh = io.open(path, "r")
	if not fh then
		return nil
	end
	local lines = {}
	for line in fh:lines() do
		lines[#lines + 1] = line
	end
	fh:close()
	return lines
end

-- Ordered most specific first; the first match wins.
local NAME_PATTERNS = {
	"local%s+function%s+([%w_]+)%s*%(", -- local function foo(
	"function%s+([%w_%.:]+)%s*%(", -- function M.foo( / function M:foo(
	"local%s+([%w_]+)%s*=%s*function", -- local foo = function
	"([%w_%.:]+)%s*=%s*function%s*%(", -- M.foo = function(
	"%[[\"']([^\"']+)[\"']%]%s*=%s*function", -- t["foo"] = function
	"([%w_]+)%s*=%s*function", -- foo = function
}

--- Best-effort function name from the source line where it is defined.
--- @param src string|nil the source line
--- @param lineno integer 1-based definition line
--- @return string
local function function_name(src, lineno)
	if type(src) == "string" then
		for _, pattern in ipairs(NAME_PATTERNS) do
			local name = src:match(pattern)
			if name then
				return name
			end
		end
	end
	-- No recognisable definition form: an anonymous callback, a method in a
	-- table literal, or a multi-line signature. Label it by position rather
	-- than guessing.
	return "anonymous@" .. tostring(lineno)
end

--- Walk a prototype tree collecting one record per function.
--- Each record carries the definition line and that prototype's OWN executable
--- lines -- child prototypes contribute their own records, so a parent's set
--- excludes its nested closures.
local function collect_protos(proto, out, depth, util)
	if depth > MAX_PROTO_DEPTH then
		return out
	end

	local own = {}
	local pc = 0
	while true do
		local ok, bc = pcall(util.funcbc, proto, pc)
		if not ok or bc == nil then
			break
		end
		local ok_info, info = pcall(util.funcinfo, proto, pc)
		if ok_info and info and info.currentline and info.currentline > 0 then
			own[info.currentline] = true
		end
		pc = pc + 1
	end

	local ok_fi, fi = pcall(util.funcinfo, proto)
	-- linedefined == 0 is the file's main chunk, not a function. lcov reports
	-- functions; including the chunk would inflate FNF on every file.
	if ok_fi and fi and fi.linedefined and fi.linedefined > 0 then
		out[#out + 1] = { line = fi.linedefined, own = own }
	end

	local i = -1
	while true do
		local ok, k = pcall(util.funck, proto, i)
		if not ok or k == nil then
			break
		end
		if type(k) == "proto" then
			collect_protos(k, out, depth + 1, util)
		end
		i = i - 1
	end

	return out
end

--- Build the function records for one file.
--- @param path string
--- @param chunk function compiled chunk for path
--- @param hits table<integer, integer> line -> count for this file
--- @param util table jit.util
local function record_functions(path, chunk, hits, util)
	local protos = collect_protos(chunk, {}, 0, util)
	if #protos == 0 then
		return
	end

	local src = read_source_lines(path)
	local records = {}
	for _, proto in ipairs(protos) do
		-- The definition line executes when the closure is CREATED, not when it
		-- is called, so counting it would report every function as covered.
		-- Use the first body line strictly after it instead.
		local first_body
		for line in pairs(proto.own) do
			if line > proto.line and (first_body == nil or line < first_body) then
				first_body = line
			end
		end

		local count = 0
		if first_body and hits[first_body] then
			count = hits[first_body]
		end

		records[#records + 1] = {
			name = function_name(src and src[proto.line] or nil, proto.line),
			line = proto.line,
			count = count,
		}
	end
	_neospec_functions[path] = records
end

--- Record files that no test ever loaded.
---
--- The line hook only ever sees a file that was required, so a module no test
--- touches is absent from the report entirely rather than present at 0%. That
--- silently flatters the total: the least-tested code in a project is exactly
--- the code most likely never to be loaded.
---
--- paths is a resolved list supplied by the caller. Discovery stays on the Go
--- side, which already globs test files and knows the project root -- the
--- include patterns here are substrings and cannot be walked.
--- @param paths string[] absolute paths to source files
--- @param util table jit.util
local function add_never_loaded(paths, util)
	-- Compare canonical paths, not raw strings. The hook keys entries by
	-- whatever debug.getinfo reported, which depends on how the module was
	-- required; the caller supplies absolute paths. Adding a file that is
	-- already covered under a different spelling would count its lines twice
	-- and depress the percentage.
	local seen = {}
	local function canonical(p)
		if vim and vim.uv and vim.uv.fs_realpath then
			local real = vim.uv.fs_realpath(p)
			if real then
				return real
			end
		end
		return p
	end
	for path in pairs(_neospec_coverage) do
		seen[canonical(path)] = true
	end

	for _, path in ipairs(paths) do
		if type(path) == "string" and #path > 0 and not seen[canonical(path)] then
			local chunk = loadfile(path)
			if chunk then
				local executable = collect_proto_lines(chunk, {}, 0, util)
				-- Only create an entry when the file has executable lines. A file
				-- of pure comments would otherwise appear as 0/0, which renders
				-- as 0% in some reporters and misrepresents it as untested.
				local entry, any = {}, false
				for line in pairs(executable) do
					entry[line] = 0
					any = true
				end
				if any then
					_neospec_coverage[path] = entry
					record_functions(path, chunk, entry, util)
				end
			end
		end
	end
end

--- Fill in zero counts for executable lines that were never executed.
---
--- Only touches files already present in _neospec_coverage -- a file no test
--- ever loaded has no entry and stays absent, because the hook never saw it.
--- Recording it would require walking the source tree, which is the caller's
--- job via its own include patterns.
---
--- Safe to call more than once: lines that already have a count keep it.
function _neospec_coverage_finalize()
	if type(_neospec_coverage) ~= "table" then
		return
	end

	-- jit.util is LuaJIT-specific. Neovim always embeds LuaJIT, but degrade to
	-- the previous hits-only behaviour rather than erroring if it is absent.
	local ok_util, util = pcall(require, "jit.util")
	if not ok_util or type(util) ~= "table" or type(util.funcbc) ~= "function" then
		return
	end

	for path, lines in pairs(_neospec_coverage) do
		if type(lines) == "table" then
			-- loadfile compiles without executing, so this cannot re-run any
			-- module side effects. A file that has moved or become unreadable
			-- since it was required simply keeps its hits-only data.
			local chunk = loadfile(path)
			if chunk then
				local executable = collect_proto_lines(chunk, {}, 0, util)
				for line in pairs(executable) do
					if lines[line] == nil then
						lines[line] = 0
					end
				end
				-- Functions are derived after the zero-fill so a function whose
				-- body never ran still resolves a count (0) rather than nil.
				record_functions(path, chunk, lines, util)
			end
		end
	end

	-- Files no test loaded are invisible to the hook; add them from the
	-- caller-supplied list so they count against the total.
	if type(_neospec_coverage_sources) == "table" then
		add_never_loaded(_neospec_coverage_sources, util)
	end
end

-- Note: debug.sethook sets the hook for the current thread only. Code executed
-- inside coroutines is not covered. This is a known limitation of the
-- single-thread hook model; coroutine-heavy test suites will report lower
-- coverage than actual execution.
debug.sethook(hook, "l")
