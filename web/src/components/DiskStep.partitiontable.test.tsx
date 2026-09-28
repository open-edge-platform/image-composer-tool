// Partition-table control tests for the Disk step.
//
// MBR stays in the schema's enum and the builder still implements it, so the
// chip is rendered — the guarantee here is that it cannot be *chosen*, and that
// a template already declaring it is not quietly redrawn as GPT.
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import {
  PARTITION_TABLE_DISABLED,
  PARTITION_TABLE_TYPES,
  isPartitionTableSelectable,
} from '../lib/disk'
import type { PartitionTableType } from '../lib/disk'

// A minimal stand-in for the step's chip row, built from the same exports the
// component renders from. It keeps the assertions on the rule rather than on
// the surrounding step, which needs the whole store to mount.
function PartitionTableChips({ current }: { current: PartitionTableType }) {
  return (
    <div>
      {PARTITION_TABLE_TYPES.map((t) => {
        const selected = current === t
        const lockedReason = selected ? undefined : PARTITION_TABLE_DISABLED[t]
        return (
          <button key={t} type="button" disabled={lockedReason !== undefined} title={lockedReason}>
            {t.toUpperCase()}
          </button>
        )
      })}
    </div>
  )
}

// chip returns the rendered button for a table type. jest-dom is not installed,
// so assertions read the DOM properties directly.
function chip(label: string): HTMLButtonElement {
  return screen.getByRole('button', { name: label }) as HTMLButtonElement
}

describe('partition table selectability', () => {
  it('offers GPT and locks MBR', () => {
    expect(isPartitionTableSelectable('gpt')).toBe(true)
    expect(isPartitionTableSelectable('mbr')).toBe(false)
  })

  it('states a reason for the locked type', () => {
    expect(PARTITION_TABLE_DISABLED.mbr).toBeTruthy()
    expect(PARTITION_TABLE_DISABLED.gpt).toBeUndefined()
  })

  it('renders MBR disabled when GPT is selected', () => {
    render(<PartitionTableChips current="gpt" />)

    expect(chip('GPT').disabled).toBe(false)
    expect(chip('MBR').disabled).toBe(true)
    expect(chip('MBR').title).toBe(PARTITION_TABLE_DISABLED.mbr)
  })

  // A template that already declares MBR must still render it as its own value,
  // so the step reports what will be built rather than what is offered.
  it('leaves MBR enabled while it is the template’s current value', () => {
    render(<PartitionTableChips current="mbr" />)

    expect(chip('MBR').disabled).toBe(false)
    expect(chip('GPT').disabled).toBe(false)
  })
})
