import { redirect } from "next/navigation";

// The public marketing site is gone. Proxy redirects `/` as well; this covers
// a request that reaches the app router without that redirect.
export default function RootPage() {
  redirect("/login");
}
