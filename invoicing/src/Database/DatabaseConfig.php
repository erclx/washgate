<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Database;

use InvalidArgumentException;
use PDO;

final readonly class DatabaseConfig
{
    private const array VARIABLES = ['DB_HOST', 'DB_PORT', 'DB_NAME', 'DB_USER', 'DB_PASSWORD'];

    public function __construct(
        public string $host,
        public string $port,
        public string $name,
        public string $user,
        public string $password,
    ) {}

    /** @param array<string, mixed> $environment */
    public static function fromEnvironment(array $environment): self
    {
        foreach (self::VARIABLES as $variable) {
            if (!isset($environment[$variable]) || !is_string($environment[$variable])) {
                throw new InvalidArgumentException("Environment variable {$variable} is required to reach the database.");
            }
        }

        return new self(
            (string) $environment['DB_HOST'],
            (string) $environment['DB_PORT'],
            (string) $environment['DB_NAME'],
            (string) $environment['DB_USER'],
            (string) $environment['DB_PASSWORD'],
        );
    }

    public function connect(): PDO
    {
        $pdo = new PDO(
            "mysql:host={$this->host};port={$this->port};dbname={$this->name};charset=utf8mb4",
            $this->user,
            $this->password,
            [
                PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION,
                PDO::ATTR_DEFAULT_FETCH_MODE => PDO::FETCH_ASSOC,
                PDO::ATTR_EMULATE_PREPARES => false,
            ],
        );
        $pdo->exec("SET time_zone = '+00:00'");

        return $pdo;
    }
}
