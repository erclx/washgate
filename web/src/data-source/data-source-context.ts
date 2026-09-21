import { createContext, useContext } from 'react'

import type { DataSource } from './data-source'

export const DataSourceContext = createContext<DataSource | null>(null)

export function useDataSource(): DataSource {
  const source = useContext(DataSourceContext)
  if (source === null) {
    throw new Error('useDataSource needs a DataSourceContext above it')
  }
  return source
}
