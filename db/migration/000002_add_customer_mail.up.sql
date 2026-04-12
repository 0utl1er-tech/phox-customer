-- Add mail column to Customer. The representative email address for a customer
-- (in contrast to per-contact emails stored in Contact.mail).
ALTER TABLE "Customer" ADD COLUMN "mail" varchar NOT NULL DEFAULT '';
