-- AlterTable
ALTER TABLE "payment_intents" ADD COLUMN     "refunded_cents" BIGINT NOT NULL DEFAULT 0;
