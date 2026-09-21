<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Tests\Database;

use PDO;
use PHPUnit\Framework\TestCase;
use RuntimeException;

/** A throwaway MariaDB database carrying the schema central's migrations build. */
final class TemporaryDatabase
{
    private function __construct(
        private readonly PDO $server,
        private readonly PDO $pdo,
        private readonly string $name,
    ) {}

    public static function create(): self
    {
        $dsn = getenv('INVOICING_TEST_DSN');
        if ($dsn === false || $dsn === '') {
            TestCase::markTestSkipped('Set INVOICING_TEST_DSN, INVOICING_TEST_USER, and INVOICING_TEST_PASSWORD to run the MariaDB tests.');
        }
        $user = getenv('INVOICING_TEST_USER') ?: 'root';
        $password = getenv('INVOICING_TEST_PASSWORD') ?: '';
        $name = 'invoicing_test_' . bin2hex(random_bytes(6));

        $server = new PDO($dsn, $user, $password, [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]);
        $server->exec("CREATE DATABASE `{$name}` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci");

        $pdo = new PDO("{$dsn};dbname={$name}", $user, $password, [
            PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION,
            PDO::ATTR_DEFAULT_FETCH_MODE => PDO::FETCH_ASSOC,
            PDO::ATTR_EMULATE_PREPARES => true,
        ]);
        $pdo->exec("SET time_zone = '+00:00'");
        self::applyMigrations($pdo);
        $pdo->setAttribute(PDO::ATTR_EMULATE_PREPARES, false);

        return new self($server, $pdo, $name);
    }

    public function connection(): PDO
    {
        return $this->pdo;
    }

    public function drop(): void
    {
        $this->server->exec("DROP DATABASE IF EXISTS `{$this->name}`");
    }

    private static function applyMigrations(PDO $pdo): void
    {
        $files = glob(__DIR__ . '/../../../core/migrations/*.up.sql');
        if ($files === false || $files === []) {
            throw new RuntimeException('No migrations found under core/migrations.');
        }
        sort($files);
        foreach ($files as $file) {
            $sql = file_get_contents($file);
            if ($sql === false) {
                throw new RuntimeException("Cannot read migration {$file}.");
            }
            $pdo->exec($sql);
        }
    }
}
