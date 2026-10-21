import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PersonAvatar, personInitial, usableAvatarUrl } from "./person-avatar";

describe("usableAvatarUrl", () => {
  it("keeps http(s) URLs and drops everything else", () => {
    expect(usableAvatarUrl(" https://avatars.githubusercontent.com/u/1?v=4 ")).toBe(
      "https://avatars.githubusercontent.com/u/1?v=4",
    );
    expect(usableAvatarUrl("http://example.com/a.png")).toBe("http://example.com/a.png");
    expect(usableAvatarUrl("")).toBeUndefined();
    expect(usableAvatarUrl("   ")).toBeUndefined();
    expect(usableAvatarUrl(null)).toBeUndefined();
    expect(usableAvatarUrl("not a url")).toBeUndefined();
    expect(usableAvatarUrl("javascript:alert(1)")).toBeUndefined();
  });
});

describe("personInitial", () => {
  it("uses the first letter, or a mark when the name is missing", () => {
    expect(personInitial("octocat")).toBe("O");
    expect(personInitial("  ada ")).toBe("A");
    expect(personInitial("")).toBe("?");
    expect(personInitial(undefined)).toBe("?");
  });
});

describe("PersonAvatar", () => {
  it("renders an initial instead of an empty circle when the URL is missing", () => {
    render(<PersonAvatar name="octocat" />);
    expect(screen.getByText("O")).toBeTruthy();
    expect(document.querySelector("img")).toBeNull();
  });

  it("replaces a failed image with the initial", () => {
    render(<PersonAvatar name="hubot" url="https://avatars.githubusercontent.com/u/0?v=4" />);
    const img = document.querySelector("img");
    expect(img).toBeTruthy();
    fireEvent.error(img!);
    expect(screen.getByText("H")).toBeTruthy();
    expect(document.querySelector("img")).toBeNull();
  });
});
