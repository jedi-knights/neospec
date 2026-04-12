-- harness.lua
-- Minimal BDD test harness: describe / it / before_each / after_each.
-- Results accumulate in _neospec_results for later serialization.
--
-- Requires Neovim 0.10+ (stable). vim.uv is used for high-resolution timing;
-- it does not exist on 0.9.x and earlier, which would produce a nil-index
-- crash on the hrtime call. neospec's neovim_version default is "stable".

-- Guard against double-load: if the harness is sourced a second time (e.g.
-- from a user init_file that explicitly sources harness.lua), skip
-- re-defining the DSL globals so any monkey-patches applied by the test file
-- are preserved and accumulated results are not discarded.
if _neospec_harness_loaded == true then
	-- Re-validate _neospec_results on each load attempt. The `or {}` pattern
	-- only repairs nil/false; a truthy non-table (e.g. a number set by a
	-- misbehaving test) bypasses it and causes table.insert to crash later.
	if type(_neospec_results) ~= "table" then
		_neospec_results = {}
	end
	return
end
_neospec_harness_loaded = true

-- Re-validate _neospec_results at first load. The `or {}` pattern only repairs
-- nil/false; a truthy non-table (e.g. a number) set before the harness loads
-- would bypass it and cause table.insert to crash on the first recorded result.
if type(_neospec_results) ~= "table" then
	_neospec_results = {}
end

-- Fail fast with a clear diagnostic on Neovim < 0.10. vim.uv was introduced in
-- 0.10 (stable) and is required for high-resolution timing. Without this guard
-- the nil-index crash surfaces deep inside it() with no version context.
if not vim.uv then
	error("neospec requires Neovim 0.10 or later (vim.uv is not available)", 0)
end

local _describe_stack = {}
local _before_each_stack = {}
local _after_each_stack = {}

-- full_test_name builds the fully-qualified test name by joining the current
-- describe stack with the given leaf name. Used by both it() and pending().
local function full_test_name(name)
	local prefix = #_describe_stack > 0 and (table.concat(_describe_stack, " > ") .. " > ") or ""
	return prefix .. name
end

-- describe groups related tests. Blocks may be nested.
function describe(name, fn)
	table.insert(_describe_stack, name)
	table.insert(_before_each_stack, {})
	table.insert(_after_each_stack, {})

	local ok, err = pcall(fn)
	if not ok then
		-- A describe block that fails at setup time counts as an error.
		table.insert(_neospec_results, {
			name = table.concat(_describe_stack, " > "),
			status = "error",
			output = "",
			error = tostring(err),
			duration_ms = 0,
		})
	end

	table.remove(_before_each_stack)
	table.remove(_after_each_stack)
	table.remove(_describe_stack)
end

-- before_each registers a setup function for the current describe block.
-- It must be called inside a describe block; calling it at the top level
-- would index nil in _before_each_stack and crash.
function before_each(fn)
	local hooks = _before_each_stack[#_before_each_stack]
	if not hooks then
		error("before_each called outside a describe block", 2)
	end
	table.insert(hooks, fn)
end

-- after_each registers a teardown function for the current describe block.
-- It must be called inside a describe block; calling it at the top level
-- would index nil in _after_each_stack and crash.
function after_each(fn)
	local hooks = _after_each_stack[#_after_each_stack]
	if not hooks then
		error("after_each called outside a describe block", 2)
	end
	table.insert(hooks, fn)
end

-- it defines a single test case.
function it(name, fn)
	if #_describe_stack == 0 then
		error("it called outside a describe block", 2)
	end
	local full_name = full_test_name(name)
	-- output_buf collects print() output captured during the test.
	-- Print capture is not yet implemented; the field is reserved for a future
	-- version. All results currently report output = "".
	local output_buf = {}

	-- Run all before_each hooks (outermost first). level_idx tracks which level
	-- is currently executing so that on failure the after_each teardown only runs
	-- for levels 1 through level_idx — levels whose before_each hooks actually
	-- executed. Running teardown for deeper levels whose before_each never ran
	-- would invoke after_each for state that was never set up.
	for level_idx, hooks in ipairs(_before_each_stack) do
		for _, hook in ipairs(hooks) do
			local ok, err = pcall(hook)
			if not ok then
				table.insert(_neospec_results, {
					name = full_name,
					status = "error",
					output = table.concat(output_buf, "\n"),
					error = "before_each failed: " .. tostring(err),
					duration_ms = 0,
				})
				-- Only tear down levels 1 through level_idx (innermost first).
				-- Levels beyond level_idx were never entered, so their after_each
				-- must not run.
				for i = level_idx, 1, -1 do
					for j = #_after_each_stack[i], 1, -1 do
						pcall(_after_each_stack[i][j])
					end
				end
				return
			end
		end
	end

	-- Capture raw nanoseconds and subtract before converting to milliseconds to
	-- avoid floating-point error from subtracting two large floats.
	local start_ns = vim.uv.hrtime()
	local ok, err = pcall(fn)
	local duration_ms = (vim.uv.hrtime() - start_ns) / 1e6

	-- Run all after_each hooks regardless of test outcome (innermost first).
	-- The reverse iteration is intentional: innermost describe block's teardown
	-- must run before its parent's, mirroring the setup order in reverse.
	for i = #_after_each_stack, 1, -1 do
		for j = #_after_each_stack[i], 1, -1 do
			pcall(_after_each_stack[i][j])
		end
	end

	local status = ok and "pass" or "fail"
	-- Use vim.inspect for table error objects so callers see the value rather
	-- than an unhelpful "table: 0x..." address from tostring().
	local err_str = ""
	if not ok then
		err_str = type(err) == "table" and vim.inspect(err) or tostring(err)
	end
	table.insert(_neospec_results, {
		name = full_name,
		status = status,
		output = table.concat(output_buf, "\n"),
		error = err_str,
		duration_ms = duration_ms,
	})
end

-- pending marks a test as skipped. The optional second argument is silently
-- discarded; it exists only for API compatibility with Busted's pending(),
-- where a function body can be provided but is never called.
function pending(name, _fn)
	if #_describe_stack == 0 then
		error("pending called outside a describe block", 2)
	end
	local full_name = full_test_name(name)
	table.insert(_neospec_results, {
		name = full_name,
		status = "skip",
		output = "",
		error = "",
		duration_ms = 0,
	})
end

-- assert namespace — mirrors common assertion libraries.
-- Lua's built-in assert is a function (truthy), so "assert or {}" would silently
-- keep the function rather than creating a table, causing "attempt to index
-- global 'assert'" errors. Check the type explicitly instead.
--
-- In stock Neovim --headless, assert is always Lua's built-in function.
-- If a user's init_file already set assert to a table (e.g., a compatibility
-- shim), we shallow-copy it rather than mutating it in-place so that the
-- original shared object is not modified by the harness's method additions.
--
-- The __call metamethod preserves backward compatibility with test files that
-- use bare assert(v, msg) rather than the assert.* API — without it, test
-- files that call assert(condition) would get "attempt to call a table value".
local _builtin_assert = assert
if type(assert) ~= "table" then
	assert = setmetatable({}, {
		__call = function(_, ...)
			return _builtin_assert(...)
		end,
	})
else
	local src_mt = getmetatable(assert)
	-- "force" is semantically clearer than "keep" when the destination is always
	-- an empty table: there are no conflicts to resolve, we are simply copying.
	assert = vim.tbl_deep_extend("force", {}, assert)
	-- vim.tbl_extend returns a new table without copying the source metatable.
	-- Restore the original metatable if one existed; otherwise install a __call
	-- metamethod so that bare assert(v, msg) continues to work after the replace.
	if src_mt then
		setmetatable(assert, src_mt)
	else
		setmetatable(assert, {
			__call = function(_, ...)
				return _builtin_assert(...)
			end,
		})
	end
end

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
		error(msg or string.format("expected %s, got %s", fmt_val(expected), fmt_val(actual)), 2)
	end
end

function assert.not_equals(unexpected, actual, msg)
	if unexpected == actual then
		error(msg or string.format("expected value to differ from %s", fmt_val(unexpected)), 2)
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

-- assert.has_error asserts that fn raises any error. The second argument is
-- the failure message shown when fn does NOT error — it is NOT a pattern matched
-- against the error value. To also assert the error content, use assert.matches
-- on the result of a manual pcall.
function assert.has_error(fn, msg)
	local ok = pcall(fn)
	if ok then
		error(msg or "expected function to raise an error", 2)
	end
end

function assert.matches(pattern, str, msg)
	if type(pattern) ~= "string" then
		error(msg or string.format("expected a string pattern, got %s", type(pattern)), 2)
	end
	if type(str) ~= "string" then
		error(msg or string.format("expected a string to match against, got %s", type(str)), 2)
	end
	if not str:match(pattern) then
		error(msg or string.format("expected %q to match pattern %q", str, pattern), 2)
	end
end

-- fmt_val formats a value for assertion error messages. Tables are rendered with
-- vim.inspect so callers see structured content rather than an opaque address
-- ("table: 0x...") that tostring() would produce.
local function fmt_val(v)
	return type(v) == "table" and vim.inspect(v) or tostring(v)
end

-- deep_equal is a module-local helper used by assert.same. It is defined at
-- module scope (not inside assert.same) so it is allocated once per harness
-- load rather than once per assert.same call.
-- The seen table guards against self-referential cycles: if the same pair is
-- encountered again during recursion it is treated as equal (the same cycle
-- exists on both sides), preventing a stack overflow.
local function deep_equal(a, b, seen)
	seen = seen or {}
	if type(a) ~= type(b) then
		return false
	end
	if type(a) ~= "table" then
		return a == b
	end
	-- Cycle guard: record both directions (a→b and b→a) so that mutual-reference
	-- structures (A.x=B, B.x=A) are detected immediately on the reverse visit
	-- without descending one extra level of recursion. Only the forward entry is
	-- strictly necessary to prevent infinite loops, but the symmetric entry avoids
	-- the extra stack depth on complex cyclic graphs.
	if not seen[a] then
		seen[a] = {}
	end
	if seen[a][b] then
		return true
	end
	seen[a][b] = true
	if not seen[b] then
		seen[b] = {}
	end
	seen[b][a] = true
	for k, v in pairs(a) do
		if not deep_equal(v, b[k], seen) then
			return false
		end
	end
	for k in pairs(b) do
		if a[k] == nil then
			return false
		end
	end
	return true
end

-- assert.same performs a deep equality check. Unlike assert.equals, tables are
-- compared by value rather than by reference, matching Plenary/Busted behaviour.
function assert.same(expected, actual, msg)
	if not deep_equal(expected, actual, {}) then
		error(msg or string.format("expected %s, got %s", vim.inspect(expected), vim.inspect(actual)), 2)
	end
end

-- assert.truthy / assert.is_truthy pass for any value that Lua considers truthy
-- (everything except false and nil).
function assert.truthy(v, msg)
	if not v then
		error(msg or ("expected truthy value, got " .. tostring(v)), 2)
	end
end

-- Defined as a wrapper function rather than a snapshot alias so that
-- monkeypatching assert.truthy (e.g. for spy purposes) is reflected here too.
function assert.is_truthy(v, msg)
	return assert.truthy(v, msg)
end

-- Type-checking assertions that mirror Plenary/Busted's assert.is_* family.
function assert.is_table(v, msg)
	if type(v) ~= "table" then
		error(msg or ("expected table, got " .. type(v)), 2)
	end
end

function assert.is_function(v, msg)
	if type(v) ~= "function" then
		error(msg or ("expected function, got " .. type(v)), 2)
	end
end

function assert.is_string(v, msg)
	if type(v) ~= "string" then
		error(msg or ("expected string, got " .. type(v)), 2)
	end
end

function assert.is_boolean(v, msg)
	if type(v) ~= "boolean" then
		error(msg or ("expected boolean, got " .. type(v)), 2)
	end
end
