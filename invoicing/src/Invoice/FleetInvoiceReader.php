<?php

declare(strict_types=1);

namespace Washgate\Invoicing\Invoice;

use Closure;
use PDO;
use PDOException;

/** Reads a month of fleet washes from the ledger. Never writes. */
final class FleetInvoiceReader
{
    private const string FLEET_PRICE_CODE = 'fleet_wash';

    private const string SQL = <<<'SQL'
        SELECT c.name AS company_name,
               w.plate AS plate,
               v.leasing_company AS leasing_company,
               COUNT(*) AS wash_count,
               COALESCE(SUM(p.amount_ore), 0) AS amount_ore,
               SUM(p.code IS NULL) AS unpriced_count
        FROM washes w
        JOIN companies c ON c.id = w.company_id
        LEFT JOIN vehicles v ON v.plate = w.plate
        LEFT JOIN prices p ON p.code = :price_code
            AND p.valid_from = (
                SELECT MAX(latest.valid_from)
                FROM prices latest
                WHERE latest.code = p.code AND latest.valid_from <= w.admitted_at
            )
        WHERE w.plan = 'fleet' AND w.admitted_at >= :range_start AND w.admitted_at < :range_end
        GROUP BY w.company_id, c.name, w.plate, v.leasing_company
        ORDER BY c.name, w.plate
        SQL;

    /** @param Closure(): PDO $connect */
    public function __construct(private readonly Closure $connect) {}

    /**
     * @return list<InvoiceLine>
     *
     * @throws MissingPriceException when a wash was admitted with no fleet price in force
     * @throws PDOException          when the connection or the query fails
     */
    public function read(BillingMonth $month): array
    {
        $statement = ($this->connect)()->prepare(self::SQL);
        $statement->execute([
            'price_code' => self::FLEET_PRICE_CODE,
            'range_start' => $month->startUtc()->format('Y-m-d H:i:s.u'),
            'range_end' => $month->endUtc()->format('Y-m-d H:i:s.u'),
        ]);

        $lines = [];
        /** @var array{company_name: string, plate: string, leasing_company: ?string, wash_count: int|string, amount_ore: int|string, unpriced_count: int|string} $row */
        foreach ($statement->fetchAll() as $row) {
            if ((int) $row['unpriced_count'] > 0) {
                throw MissingPriceException::forCompany($row['company_name']);
            }
            $lines[] = new InvoiceLine(
                $row['company_name'],
                $row['plate'],
                $row['leasing_company'],
                (int) $row['wash_count'],
                (int) $row['amount_ore'],
            );
        }

        return $lines;
    }
}
