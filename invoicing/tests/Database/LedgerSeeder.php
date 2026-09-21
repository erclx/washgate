<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Tests\Database;

use PDO;

/** Inserts the ledger rows a test needs, with UTC timestamps as central stores them. */
final class LedgerSeeder
{
    private int $washCount = 0;

    public function __construct(private readonly PDO $pdo)
    {
        $this->pdo->exec("INSERT INTO sites (id, name) VALUES ('site-1', 'Site One')");
    }

    public function company(string $id, string $name): void
    {
        $this->insert('INSERT INTO companies (id, name) VALUES (?, ?)', [$id, $name]);
    }

    public function vehicle(string $plate, ?string $companyId, ?string $leasingCompany): void
    {
        $this->insert(
            'INSERT INTO vehicles (plate, company_id, leasing_company) VALUES (?, ?, ?)',
            [$plate, $companyId, $leasingCompany],
        );
    }

    public function price(string $code, int $amountOre, string $validFrom): void
    {
        $this->insert('INSERT INTO prices (code, amount_ore, valid_from) VALUES (?, ?, ?)', [$code, $amountOre, $validFrom]);
    }

    public function fleetWash(string $plate, string $companyId, string $admittedAtUtc): void
    {
        $this->wash($plate, 'fleet', $companyId, $admittedAtUtc);
    }

    public function premiumWash(string $plate, string $admittedAtUtc): void
    {
        $this->wash($plate, 'premium', null, $admittedAtUtc);
    }

    private function wash(string $plate, string $plan, ?string $companyId, string $admittedAtUtc): void
    {
        ++$this->washCount;
        $this->insert(
            'INSERT INTO washes (id, site_id, plate, plan, company_id, admitted_at, received_at) VALUES (?, ?, ?, ?, ?, ?, ?)',
            ["wash-{$this->washCount}", 'site-1', $plate, $plan, $companyId, $admittedAtUtc, $admittedAtUtc],
        );
    }

    /** @param list<string|int|null> $values */
    private function insert(string $sql, array $values): void
    {
        $this->pdo->prepare($sql)->execute($values);
    }
}
