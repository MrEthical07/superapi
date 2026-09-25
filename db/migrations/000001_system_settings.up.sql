-- Intentionally a no-op since v0.9.0.
--
-- Earlier releases created a demo `system_settings` table here for the removed
-- system module. The file is kept (not renumbered) so migration history stays
-- valid for databases that already applied it; new databases no longer get the
-- table. The down migration still drops it if present.
SELECT 1;
