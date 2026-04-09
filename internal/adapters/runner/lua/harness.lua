-- harness.lua
-- Minimal BDD test harness: describe / it / before_each / after_each.
-- Results accumulate in _neospec_results for later serialization.

_neospec_results = {}

local _describe_stack = {}
local _before_each_stack = {}
local _after_each_stack = {}

-- describe groups related tests. Blocks may be nested.
function describe(name, fn)
  table.insert(_describe_stack, name)
  table.insert(_before_each_stack, {})
  table.insert(_after_each_stack, {})

  local ok, err = pcall(fn)
  if not ok then
    -- A describe block that fails at setup time counts as an error.
    table.insert(_neospec_results, {
      name   = table.concat(_describe_stack, " > "),
      status = "error",
      output = "",
      error  = tostring(err),
      duration_ms = 0,
    })
  end

  table.remove(_before_each_stack)
  table.remove(_after_each_stack)
  table.remove(_describe_stack)
end

-- before_each registers a setup function for the current describe block.
function before_each(fn)
  local hooks = _before_each_stack[#_before_each_stack]
  table.insert(hooks, fn)
end

-- after_each registers a teardown function for the current describe block.
function after_each(fn)
  local hooks = _after_each_stack[#_after_each_stack]
  table.insert(hooks, fn)
end

-- it defines a single test case.
function it(name, fn)
  local full_name = table.concat(_describe_stack, " > ") .. " > " .. name
  local output_buf = {}

  -- Run all before_each hooks (outermost first).
  for _, hooks in ipairs(_before_each_stack) do
    for _, hook in ipairs(hooks) do
      local ok, err = pcall(hook)
      if not ok then
        table.insert(_neospec_results, {
          name   = full_name,
          status = "error",
          output = table.concat(output_buf, "\n"),
          error  = "before_each failed: " .. tostring(err),
          duration_ms = 0,
        })
        return
      end
    end
  end

  local start_ms = vim.loop.hrtime() / 1e6
  local ok, err = pcall(fn)
  local duration_ms = (vim.loop.hrtime() / 1e6) - start_ms

  -- Run all after_each hooks regardless of test outcome (innermost first).
  for i = #_after_each_stack, 1, -1 do
    for j = #_after_each_stack[i], 1, -1 do
      pcall(_after_each_stack[i][j])
    end
  end

  local status = ok and "pass" or "fail"
  table.insert(_neospec_results, {
    name        = full_name,
    status      = status,
    output      = table.concat(output_buf, "\n"),
    error       = ok and "" or tostring(err),
    duration_ms = duration_ms,
  })
end

-- pending marks a test as skipped.
function pending(name, _fn)
  local full_name = table.concat(_describe_stack, " > ") .. " > " .. name
  table.insert(_neospec_results, {
    name        = full_name,
    status      = "skip",
    output      = "",
    error       = "",
    duration_ms = 0,
  })
end

-- assert namespace — mirrors common assertion libraries.
assert = assert or {}

function assert.is_true(v, msg)
  if v ~= true then
    error(msg or ("expected true, got " .. tostring(v)), 2)
  end
end

function assert.is_false(v, msg)
  if v ~= false then
    error(msg or ("expected false, got " .. tostring(v)), 2)
  end
end

function assert.equals(expected, actual, msg)
  if expected ~= actual then
    error(msg or string.format("expected %s, got %s", tostring(expected), tostring(actual)), 2)
  end
end

function assert.not_equals(unexpected, actual, msg)
  if unexpected == actual then
    error(msg or string.format("expected value to differ from %s", tostring(unexpected)), 2)
  end
end

function assert.is_nil(v, msg)
  if v ~= nil then
    error(msg or ("expected nil, got " .. tostring(v)), 2)
  end
end

function assert.is_not_nil(v, msg)
  if v == nil then
    error(msg or "expected non-nil value", 2)
  end
end

function assert.has_error(fn, msg)
  local ok = pcall(fn)
  if ok then
    error(msg or "expected function to raise an error", 2)
  end
end

function assert.matches(pattern, str, msg)
  if not tostring(str):match(pattern) then
    error(msg or string.format("expected %q to match pattern %q", tostring(str), pattern), 2)
  end
end
