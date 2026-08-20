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
