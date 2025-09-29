"use client";

import { ReactNode } from "react";
import { ThemeToggle } from "./ThemeToggle";

interface SimpleLayoutProps {
  children: ReactNode;
  environment?: string;
}

export function SimpleLayout({ children, environment = "..." }: SimpleLayoutProps) {

  return (
    <div style={{ minHeight: "100vh", backgroundColor: "var(--bg-secondary)" }}>
      <header style={{
        display: "flex",
        justifyContent: "space-between",
        alignItems: "center",
        padding: "1rem",
        borderBottom: "1px solid var(--border-color)",
        backgroundColor: "var(--bg-color)"
      }}>
        <span style={{ fontWeight: "bold", fontSize: "1.2rem" }}>Podboard</span>
        <div style={{ display: "flex", alignItems: "center", gap: "1rem", fontSize: "0.9rem" }}>
          <span>{environment}</span>
          <ThemeToggle />
        </div>
      </header>
      <main style={{ padding: "2rem" }}>
        {children}
      </main>
    </div>
  );
}