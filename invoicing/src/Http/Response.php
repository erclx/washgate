<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Http;

final readonly class Response
{
    /** @param array<string, string> $headers */
    public function __construct(
        public int $status,
        public array $headers,
        public string $body,
    ) {}

    public static function text(int $status, string $message): self
    {
        return new self($status, ['Content-Type' => 'text/plain; charset=utf-8'], $message . "\n");
    }
}
