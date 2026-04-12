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

-- Note: debug.sethook sets the hook for the current thread only. Code executed
-- inside coroutines is not covered. This is a known limitation of the
-- single-thread hook model; coroutine-heavy test suites will report lower
-- coverage than actual execution.
debug.sethook(hook, "l")
