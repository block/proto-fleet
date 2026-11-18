import type { ReactNode } from "react";
import { type ConfigurationPoolDraft, poolSlotLabel } from "./types";
import Button from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";

export function ConfigurationForm<T extends ConfigurationPoolDraft>({
  name,
  nameReadOnly = false,
  onChangeName,
  pools,
  onChangePools,
  newPool,
  validateURL,
  maxPools = 3,
  hasSavedPassword,
  canPreservePassword,
  passwordRequired,
  usernameHelp,
  connectionTest,
  poolControls,
}: {
  name: string;
  nameReadOnly?: boolean;
  onChangeName: (name: string) => void;
  pools: T[];
  onChangePools: (update: (pools: T[]) => T[]) => void;
  newPool: () => T;
  validateURL: (url: string) => string | undefined;
  maxPools?: number;
  hasSavedPassword?: (pool: T) => boolean;
  canPreservePassword?: (pool: T) => boolean;
  passwordRequired?: (pool: T) => boolean;
  usernameHelp?: (pool: T) => ReactNode;
  connectionTest?: (pool: T) => ReactNode;
  poolControls?: (pool: T) => ReactNode;
}) {
  const update = (key: string, patch: Partial<ConfigurationPoolDraft>) =>
    onChangePools((current) => current.map((pool) => (pool.key === key ? { ...pool, ...patch } : pool)));
  const move = (key: string, by: number) => {
    onChangePools((current) => {
      const index = current.findIndex((pool) => pool.key === key);
      const destination = index + by;
      if (index < 0 || destination < 0 || destination >= current.length) return current;
      const reordered = [...current];
      [reordered[index], reordered[destination]] = [reordered[destination], reordered[index]];
      return reordered;
    });
  };
  const savedPasswordHint = (pool: T) => {
    if (canPreservePassword?.(pool)) return "The saved password stays hidden.";
    if (passwordRequired?.(pool)) return "The URL or username changed. Uncheck Keep saved password to replace it.";
    return "The URL or username changed. Uncheck Keep saved password to replace or clear it.";
  };
  return (
    <>
      <Input
        id="pool-configuration-name"
        label="Name"
        initValue={name}
        readOnly={nameReadOnly}
        onChange={onChangeName}
        maxLength={255}
      />
      {pools.map((pool, index) => (
        <div
          key={pool.key}
          role="group"
          aria-label={poolSlotLabel(index)}
          className="flex flex-col gap-4 rounded-xl border border-border-10 p-4"
          data-dd-privacy="mask"
        >
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-heading-100">{poolSlotLabel(index)}</h3>
            <div className="flex gap-3">
              {index > 0 ? <Button variant="textOnly" text="Move up" onClick={() => move(pool.key, -1)} /> : null}
              {index < pools.length - 1 ? (
                <Button variant="textOnly" text="Move down" onClick={() => move(pool.key, 1)} />
              ) : null}
              {pools.length > 1 ? (
                <Button
                  variant="textOnly"
                  text="Remove"
                  onClick={() =>
                    onChangePools((current) => {
                      if (current.length <= 1) return current;
                      return current.filter((item) => item.key !== pool.key);
                    })
                  }
                />
              ) : null}
            </div>
          </div>
          <Input
            id={`pool-url-${pool.key}`}
            label="Pool URL"
            initValue={pool.url}
            sanitize={(value) => value.trim()}
            onChange={(url) => update(pool.key, { url })}
            error={validateURL(pool.url)}
            maxLength={256}
          />
          <Input
            id={`pool-username-${pool.key}`}
            label="Username"
            initValue={pool.username}
            onChange={(username) => update(pool.key, { username })}
            maxLength={255}
          />
          {usernameHelp ? <p className="text-200 text-text-primary-50">{usernameHelp(pool)}</p> : null}
          {hasSavedPassword?.(pool) ? (
            <label className="flex items-center gap-3">
              <Checkbox
                checked={pool.preservePassword}
                onChange={(event) => update(pool.key, { preservePassword: event.target.checked })}
              />
              Keep saved password
            </label>
          ) : null}
          {pool.preservePassword ? (
            <p className="text-200 text-text-primary-50">{savedPasswordHint(pool)}</p>
          ) : (
            <Input
              id={`pool-password-${pool.key}`}
              label={passwordRequired?.(pool) ? "Pool password" : "Pool password (optional)"}
              type="password"
              initValue={pool.password}
              onChange={(password) => update(pool.key, { password })}
              autoComplete="new-password"
              maxLength={255}
            />
          )}
          {connectionTest?.(pool)}
          {poolControls?.(pool)}
        </div>
      ))}
      {pools.length < maxPools ? (
        <Button
          variant="secondary"
          text="Add backup pool"
          className="self-start"
          onClick={() => {
            const pool = newPool();
            onChangePools((current) => (current.length < maxPools ? [...current, pool] : current));
          }}
        />
      ) : null}
    </>
  );
}
