import { useQuery } from "@tanstack/react-query";
import type { ComponentProps } from "react";
import { Avatar } from "../ui/Avatar";

export function LiveAvatar({
  accountID,
  ...props
}: ComponentProps<typeof Avatar> & { accountID: string }) {
  const revision = useQuery({
    queryKey: ["avatars", accountID],
    queryFn: () => Date.now(),
    initialData: 0,
    staleTime: Infinity,
  }).data;
  return (
    <Avatar
      {...props}
      url={
        props.url && revision
          ? `${props.url}${props.url.includes("?") ? "&" : "?"}v=${revision}`
          : props.url
      }
    />
  );
}
