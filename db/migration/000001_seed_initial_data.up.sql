-- Insert Company
INSERT INTO "Company" (id, name, updated_at, created_at)
VALUES (
    '0f036454-617a-493c-ae1f-f05efcbbb330',
    '0UTL1ER株式会社'
);

-- Insert User
INSERT INTO "User" (id, company_id, name, role, updated_at, created_at)
VALUES (
    'jlbeL9zvGhMB5tV2CLy8MCy3iYn2',
    '0f036454-617a-493c-ae1f-f05efcbbb330',
    '黒羽晟',
    'owner'
);
