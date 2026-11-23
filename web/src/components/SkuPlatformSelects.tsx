// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import type { ReactNode } from 'react'
import { Select } from './Select'
import type { DropdownOption, Selection } from '../store'

// The SKU and Platform dropdowns, shared by the Basic tab and the Advanced
// Target step. A use case with platformFirst asks for the platform first (see
// cascadeOrder in store.ts); each dropdown stays disabled until the one before
// it is chosen.
export function SkuPlatformSelects({
  skus,
  platforms,
  selection,
  setSel,
  skuLabel,
  platformFirst,
  required,
}: {
  skus: DropdownOption[]
  platforms: DropdownOption[]
  selection: Selection
  setSel: (k: keyof Selection, v: string) => void
  skuLabel: string
  platformFirst: boolean
  required: ReactNode
}) {
  const sku = (
    <Select
      key="sku"
      label={skuLabel}
      // SKU is optional where the manifest offers none for this use case.
      labelSuffix={skus.length > 0 ? required : undefined}
      placeholder={`-- Select ${skuLabel} --`}
      value={selection.sku}
      options={skus}
      disabled={!selection.vertical || (platformFirst && !selection.platform)}
      onChange={(v) => setSel('sku', v)}
    />
  )
  const platform = (
    <Select
      key="platform"
      label="Platform"
      labelSuffix={required}
      placeholder="-- Select Platform --"
      value={selection.platform}
      options={platforms}
      disabled={platformFirst ? !selection.vertical : !selection.sku && skus.length > 0}
      onChange={(v) => setSel('platform', v)}
    />
  )
  return <>{platformFirst ? [platform, sku] : [sku, platform]}</>
}
