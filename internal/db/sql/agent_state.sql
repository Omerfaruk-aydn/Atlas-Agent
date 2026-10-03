-- get
SELECT revision, payload FROM agent_state WHERE namespace = ? AND key = ?;
-- put
UPDATE agent_state SET payload=?,updated_at=?,revision=revision+1 WHERE namespace=? AND key=? AND revision=?;
-- create
INSERT OR IGNORE INTO agent_state(namespace,key,revision,payload,updated_at)
SELECT ?,?,1,?,? WHERE (?='goals' OR (SELECT COUNT(*) FROM agent_state WHERE namespace=?) < 256);
-- list
SELECT key, revision, payload FROM agent_state WHERE namespace=? ORDER BY key LIMIT 256;

