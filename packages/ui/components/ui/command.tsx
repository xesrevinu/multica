"use client"

import * as React from "react"
import { Combobox } from "@base-ui/react/combobox"
import { SearchIcon, CheckIcon } from "lucide-react"

import { cn } from "@multica/ui/lib/utils"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog"
import {
  InputGroup,
  InputGroupAddon,
} from "@multica/ui/components/ui/input-group"

type CommandFilter = (itemValue: string, query: string) => number

function Command({
  className,
  shouldFilter = true,
  filter,
  inputValue,
  onInputValueChange,
  children,
  ...props
}: React.ComponentProps<"div"> & {
  shouldFilter?: boolean
  filter?: CommandFilter
  inputValue?: string
  onInputValueChange?: (value: string) => void
}) {
  return (
    <Combobox.Root
      open
      autoHighlight
      filter={
        shouldFilter
          ? (itemValue, query) => {
              const text = String(itemValue ?? "")
              if (filter) return filter(text, query) > 0
              return text.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())
            }
          : null
      }
      {...(inputValue !== undefined ? { inputValue } : {})}
      {...(onInputValueChange
        ? { onInputValueChange: (value: string) => onInputValueChange(value) }
        : {})}
    >
      <div
        data-slot="command"
        className={cn(
          "flex size-full flex-col overflow-hidden rounded-xl! bg-popover p-1 text-popover-foreground",
          className
        )}
        {...props}
      >
        {children}
      </div>
    </Combobox.Root>
  )
}

function CommandDialog({
  title = "Command Palette",
  description = "Search for a command to run...",
  children,
  className,
  showCloseButton = false,
  ...props
}: Omit<React.ComponentProps<typeof Dialog>, "children"> & {
  title?: string
  description?: string
  className?: string
  showCloseButton?: boolean
  children: React.ReactNode
}) {
  return (
    <Dialog {...props}>
      <DialogHeader className="sr-only">
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
      </DialogHeader>
      <DialogContent
        className={cn(
          "top-1/3 translate-y-0 overflow-hidden rounded-xl! p-0",
          className
        )}
        showCloseButton={showCloseButton}
      >
        {children}
      </DialogContent>
    </Dialog>
  )
}

function CommandInput({
  className,
  plain = false,
  ...props
}: React.ComponentProps<typeof Combobox.Input> & { plain?: boolean }) {
  if (plain) {
    return (
      <Combobox.Input
        data-slot="command-input"
        className={cn(
          "w-full text-body outline-hidden disabled:cursor-not-allowed disabled:opacity-50",
          className
        )}
        {...props}
      />
    )
  }
  return (
    <div data-slot="command-input-wrapper" className="p-1 pb-0">
      <InputGroup className="h-8! rounded-lg! border-input/30 bg-input/30 shadow-none! *:data-[slot=input-group-addon]:pl-2!">
        <Combobox.Input
          data-slot="command-input"
          className={cn(
            "w-full text-body outline-hidden disabled:cursor-not-allowed disabled:opacity-50",
            className
          )}
          {...props}
        />
        <InputGroupAddon>
          <SearchIcon className="size-4 shrink-0 opacity-50" />
        </InputGroupAddon>
      </InputGroup>
    </div>
  )
}

function CommandList({
  className,
  ...props
}: React.ComponentProps<typeof Combobox.List>) {
  return (
    <Combobox.List
      data-slot="command-list"
      className={cn(
        "no-scrollbar max-h-72 scroll-py-1 overflow-x-hidden overflow-y-auto outline-none",
        className
      )}
      {...props}
    />
  )
}

function CommandEmpty({
  className,
  ...props
}: React.ComponentProps<typeof Combobox.Empty>) {
  return (
    <Combobox.Empty
      data-slot="command-empty"
      className={cn("py-6 text-center text-body", className)}
      {...props}
    />
  )
}

function CommandGroup({
  className,
  heading,
  children,
  ...props
}: React.ComponentProps<typeof Combobox.Group> & {
  heading?: React.ReactNode
}) {
  return (
    <Combobox.Group
      data-slot="command-group"
      className={cn(
        "overflow-hidden p-1 text-foreground",
        className
      )}
      {...props}
    >
      {heading != null && heading !== "" ? (
        <Combobox.GroupLabel
          data-slot="command-group-heading"
          className="px-2 py-1.5 text-caption font-medium text-muted-foreground"
        >
          {heading}
        </Combobox.GroupLabel>
      ) : null}
      {children}
    </Combobox.Group>
  )
}

function CommandSeparator({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="command-separator"
      className={cn("-mx-1 h-px bg-border", className)}
      {...props}
    />
  )
}

function CommandItem({
  className,
  children,
  value,
  onSelect,
  disabled,
  ...props
}: Omit<React.ComponentProps<typeof Combobox.Item>, "onSelect"> & {
  onSelect?: (value: string) => void
}) {
  const itemValue = value == null ? "" : String(value)
  return (
    <Combobox.Item
      data-slot="command-item"
      value={itemValue}
      disabled={disabled}
      onClick={() => onSelect?.(itemValue)}
      className={cn(
        "group/command-item relative flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-body outline-hidden select-none in-data-[slot=dialog-content]:rounded-lg! data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50 data-highlighted:bg-muted data-highlighted:text-foreground [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        className
      )}
      {...props}
    >
      {children}
      <CheckIcon className="ml-auto opacity-0 group-has-data-[slot=command-shortcut]/command-item:hidden group-data-[selected]/command-item:opacity-100" />
    </Combobox.Item>
  )
}

function CommandShortcut({
  className,
  ...props
}: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="command-shortcut"
      className={cn(
        "ml-auto text-caption tracking-widest text-muted-foreground group-data-highlighted/command-item:text-foreground",
        className
      )}
      {...props}
    />
  )
}

export {
  Command,
  CommandDialog,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandShortcut,
  CommandSeparator,
}
