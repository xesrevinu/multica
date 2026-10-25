import { useMemo } from "react"
import { DialogSelect, type DialogSelectOption } from "./ui/dialog-select"
import { useDialog, type DialogContext } from "./ui/dialog"

export type PickerOption = {
  id: string
  title: string
  detail?: string
  search: string
  category?: string
}

export function openPicker(
  dialog: DialogContext,
  props: {
    title: string
    placeholder?: string
    options: PickerOption[]
    selectedId?: string
    size?: "medium" | "large" | "xlarge"
    onSelect: (id: string) => void
  },
) {
  dialog.replace(() => <PickerDialog {...props} />)
}

function PickerDialog(props: {
  title: string
  placeholder?: string
  options: PickerOption[]
  selectedId?: string
  size?: "medium" | "large" | "xlarge"
  onSelect: (id: string) => void
}) {
  const dialog = useDialog()
  // replace() resets size and centering, so apply them while the dialog is mounted.
  dialog.setSize(props.size ?? "medium")
  dialog.setCentered(true)

  const grouped = useMemo(() => props.options.some((option) => option.category), [props.options])
  const options = useMemo(
    (): DialogSelectOption<string>[] =>
      props.options.map((option) => ({
        title: option.title,
        value: option.id,
        footer: option.detail,
        category: option.category,
        searchText: option.search,
      })),
    [props.options],
  )

  return (
    <DialogSelect
      title={props.title}
      placeholder={props.placeholder ?? "Search"}
      options={options}
      current={props.selectedId}
      flat
      sectionNavigation={grouped}
      onSelect={(option) => {
        dialog.clear()
        props.onSelect(option.value)
      }}
    />
  )
}
