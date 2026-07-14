import { useEffect, useRef, useState, type RefObject } from "react";

export const calculateTableScrollY = (
  containerHeight: number,
  headerHeight: number,
  minimum = 48
): number => Math.max(minimum, Math.floor(containerHeight - headerHeight));

export const useTableScrollY = (minimum = 48): [RefObject<HTMLDivElement | null>, number] => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [scrollY, setScrollY] = useState(minimum);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }

    const update = () => {
      const header = container.querySelector<HTMLElement>(".ant-table-thead");
      const next = calculateTableScrollY(
        container.clientHeight,
        header?.getBoundingClientRect().height ?? 0,
        minimum
      );
      setScrollY((current) => current === next ? current : next);
    };

    update();
    const observer = new ResizeObserver(update);
    observer.observe(container);
    return () => observer.disconnect();
  }, [minimum]);

  return [containerRef, scrollY];
};
