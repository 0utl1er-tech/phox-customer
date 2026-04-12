-- Insert Company
INSERT INTO "Company" (id, name)
VALUES (
    '0f036454-617a-493c-ae1f-f05efcbbb330',
    '0UTL1ER株式会社'
);

-- Insert Users
-- NOTE: Primary key is the Keycloak `sub` (UUID). These UUIDs are pinned in
-- phox-manifest/keycloak/realm-phox.json so re-importing the realm keeps
-- subject IDs stable across resets.
INSERT INTO "User" (id, company_id, name, role)
VALUES (
    '11111111-1111-1111-1111-111111111111',
    '0f036454-617a-493c-ae1f-f05efcbbb330',
    '黒羽晟',
    'owner'
);

INSERT INTO "User" (id, company_id, name, role)
VALUES (
    '22222222-2222-2222-2222-222222222222',
    '0f036454-617a-493c-ae1f-f05efcbbb330',
    'E2E Test User',
    'owner'
);
