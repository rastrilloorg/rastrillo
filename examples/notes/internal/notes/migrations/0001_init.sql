CREATE TABLE `users` (`id` integer PRIMARY KEY AUTOINCREMENT,`email` text,`password_hash` text,`created_at` datetime);

CREATE UNIQUE INDEX `idx_users_email` ON `users`(`email`);

CREATE TABLE `notes` (`id` integer PRIMARY KEY AUTOINCREMENT,`user_id` integer,`title` text,`body` text,`created_at` datetime,`updated_at` datetime);

CREATE INDEX `idx_notes_user_id` ON `notes`(`user_id`);

CREATE TABLE `exports` (`id` text,`owner` text,`content` text,`created_at` datetime,PRIMARY KEY (`id`));

CREATE INDEX `idx_exports_owner` ON `exports`(`owner`);

