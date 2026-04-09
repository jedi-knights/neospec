-- coverage_hook.lua
-- Installs a debug.sethook listener that records every executed Lua line.
-- Coverage data is accumulated in _neospec_coverage, keyed by source file path.
-- This file must be loaded BEFORE any code under test is required.

_neospec_coverage = {}

-- _neospec_is_test_source returns true for source files that belong to the
-- project under test (i.e. not the harness or hook itself).
local function is_project_source(source)
  -- Lua debug info prefixes sources with '@' for file paths.
  if source:sub(1, 1) ~= "@" then return false end
  local path = source:sub(2)
  -- Exclude neospec's own shim/harness files.
  if path:find("neospec_run%.lua$") then return false end
  if path:find("coverage_hook%.lua$") then return false end
  if path:find("harness%.lua$") then return false end
  if path:find("reporter%.lua$") then return false end
  return true
end

local function hook(event)
  local info = debug.getinfo(2, "Sl")
  if not info then return end
  local source = info.source or ""
  if not is_project_source(source) then return end
  local path = source:sub(2)
  local line = info.currentline
  if line < 0 then return end

  if not _neospec_coverage[path] then
    _neospec_coverage[path] = {}
  end
  _neospec_coverage[path][line] = (_neospec_coverage[path][line] or 0) + 1
end

debug.sethook(hook, "l")
