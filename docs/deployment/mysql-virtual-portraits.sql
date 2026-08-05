-- 虚拟人像素材入库任务。线上 MySQL 首次部署本功能时执行一次。
CREATE TABLE IF NOT EXISTS `virtual_portrait_tasks` (
  `id` varchar(191) NOT NULL,
  `user_id` varchar(191) NOT NULL,
  `channel_id` varchar(191) NOT NULL,
  `source_fingerprint` varchar(64) NOT NULL,
  `source_url` text NOT NULL,
  `name` varchar(191) NOT NULL DEFAULT '',
  `group_id` varchar(191) NOT NULL DEFAULT '',
  `asset_id` varchar(191) NOT NULL DEFAULT '',
  `status` varchar(32) NOT NULL DEFAULT 'processing',
  `error` text,
  `created_at` varchar(64) NOT NULL DEFAULT '',
  `updated_at` varchar(64) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_virtual_portrait_source` (`user_id`,`channel_id`,`source_fingerprint`),
  KEY `idx_virtual_portrait_tasks_user_id` (`user_id`),
  KEY `idx_virtual_portrait_tasks_channel_id` (`channel_id`),
  KEY `idx_virtual_portrait_tasks_group_id` (`group_id`),
  KEY `idx_virtual_portrait_tasks_asset_id` (`asset_id`),
  KEY `idx_virtual_portrait_tasks_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 素材 OpenAPI 的 AK/SK 保存在 settings 表 private JSON：
-- channels[].assetAccessKeyId 与 channels[].assetSecretAccessKey。
-- settings 表不需要增加独立列。
