-- Checks over the DuckDB views, read after sql/duckdb/010, 020 and 030 by CI and `make sql`.
--
-- Each check raises an error when it fails, and the CLI runs with `.bail on`, so a failing check
-- fails the step. The sample store (test/sample_store_test.go) has one actor-day without two of
-- the tools and one token count, as a source that leaves out a zero would send it.

-- An absent counter reads as 0, as ClickHouse's JSONExtractInt makes it. As NULL it would make
-- edit_accepted + multi_edit_accepted + ... NULL for the whole row, and SUM skips such a row
-- without a word: the fleet's acceptance would be undercounted, or NULL when every row lacks it.
SELECT CASE WHEN count(*) > 0 THEN error('silver_actor_day has a NULL counter; an absent field must read as 0') END AS silver_counters
FROM silver_actor_day
WHERE sessions IS NULL OR lines_added IS NULL OR lines_removed IS NULL OR commits IS NULL
   OR pull_requests IS NULL
   OR edit_accepted IS NULL OR edit_rejected IS NULL
   OR multi_edit_accepted IS NULL OR multi_edit_rejected IS NULL
   OR write_accepted IS NULL OR write_rejected IS NULL
   OR notebook_accepted IS NULL OR notebook_rejected IS NULL;

SELECT CASE WHEN count(*) > 0 THEN error('silver_actor_day_model has a NULL token count or cost; an absent field must read as 0') END AS silver_model_counters
FROM silver_actor_day_model
WHERE tokens_input IS NULL OR tokens_output IS NULL OR tokens_cache_read IS NULL
   OR tokens_cache_creation IS NULL OR cost_cents IS NULL OR cost_usd IS NULL;

SELECT CASE WHEN count(*) > 0 THEN error('gold_tool_acceptance has a NULL total') END AS gold_acceptance_totals
FROM gold_tool_acceptance
WHERE accepted IS NULL OR rejected IS NULL OR proposals IS NULL;

SELECT 'checks passed';
